package database_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

func TestTaskRepositoryLeasesFenceWorkersAndRecovery(t *testing.T) {
	t.Parallel()

	fixture := newTaskTestFixture(t)
	ctx, tasks := t.Context(), fixture.tasks
	owner := fixture.createOwner(t.Context())
	created, err := tasks.Create(ctx, newTask(owner.ID))
	require.NoError(t, err)

	expires := time.Now().Add(time.Minute)
	claimed, err := tasks.ClaimQueued(ctx, &database.TaskClaim{
		TaskID: created.ID, LeaseOwner: "worker-one", EventKind: taskStartedEvent, LeaseExpiresAt: expires,
	})
	require.NoError(t, err)
	assert.True(t, claimed)
	claimed, err = tasks.ClaimQueued(ctx, &database.TaskClaim{
		TaskID: created.ID, LeaseOwner: "worker-two", EventKind: taskStartedEvent, LeaseExpiresAt: expires,
	})
	require.NoError(t, err)
	assert.False(t, claimed)

	renewed, err := tasks.RenewLease(ctx, created.ID, "worker-two", expires.Add(time.Minute))
	require.NoError(t, err)
	assert.False(t, renewed)
	renewed, err = tasks.RenewLease(ctx, created.ID, "worker-one", expires.Add(time.Minute))
	require.NoError(t, err)
	assert.True(t, renewed)

	recovered, err := tasks.RecoverExpired(ctx, &database.TaskRecovery{
		Kind: database.TaskKindAgent, TargetState: database.TaskInterrupted,
		EventKind: taskInterruptedEvent, ErrorCode: "process_restart", ErrorMessage: "expired",
		PayloadJSON: `{"error_code":"process_restart"}`, ExpiresBefore: expires,
	})
	require.NoError(t, err)
	assert.Empty(t, recovered)

	transitioned, err := tasks.Transition(
		ctx, created.ID, []database.TaskState{database.TaskRunning}, database.TaskFailed, taskFailedEvent,
	)
	require.NoError(t, err)
	assert.False(t, transitioned)

	finish := func(leaseOwner string) (bool, error) {
		return tasks.Finish(ctx, &database.TaskFinish{
			TaskID: created.ID, From: []database.TaskState{database.TaskRunning},
			TargetState: database.TaskSucceeded, EventKind: taskSucceededEvent, Result: "", ErrorCode: "",
			ErrorMessage: "", PayloadJSON: `{}`, LeaseOwner: leaseOwner,
		})
	}
	finished, err := finish("")
	require.NoError(t, err)
	assert.False(t, finished)
	finished, err = finish("worker-two")
	require.NoError(t, err)
	assert.False(t, finished)
	finished, err = finish("worker-one")
	require.NoError(t, err)
	assert.True(t, finished)

	loaded, found, err := tasks.Get(ctx, created.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, database.TaskSucceeded, loaded.State)
	assert.Empty(t, loaded.LeaseOwner)
	assert.Nil(t, loaded.LeaseExpiresAt)
}

func TestTaskRepositoryRecoversExpiredLease(t *testing.T) {
	t.Parallel()

	fixture := newTaskTestFixture(t)
	ctx, tasks := t.Context(), fixture.tasks
	owner := fixture.createOwner(t.Context())
	created, err := tasks.Create(ctx, newTask(owner.ID))
	require.NoError(t, err)

	expires := time.Now().Add(-time.Minute)
	claimed, err := tasks.ClaimQueued(ctx, &database.TaskClaim{
		TaskID: created.ID, LeaseOwner: "dead-worker", EventKind: taskStartedEvent, LeaseExpiresAt: expires,
	})
	require.NoError(t, err)
	require.True(t, claimed)

	recovered, err := tasks.RecoverExpired(ctx, &database.TaskRecovery{
		Kind: database.TaskKindAgent, TargetState: database.TaskInterrupted,
		EventKind: taskInterruptedEvent, ErrorCode: "process_restart", ErrorMessage: "expired",
		PayloadJSON: `{"error_code":"process_restart"}`, ExpiresBefore: time.Now(),
	})
	require.NoError(t, err)
	assert.Equal(t, []string{created.ID}, recovered)

	loaded, found, err := tasks.Get(ctx, created.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, database.TaskInterrupted, loaded.State)
	assert.Empty(t, loaded.LeaseOwner)
	assert.Nil(t, loaded.LeaseExpiresAt)
}

func TestTaskRepositoryLeaseValidation(t *testing.T) {
	t.Parallel()

	fixture := newTaskTestFixture(t)
	task := fixture.createOwner(t.Context())
	expires := time.Now().Add(time.Minute)
	tests := []struct {
		run       func() error
		name      string
		wantError string
	}{
		{name: "nil claim", wantError: leaseOwnerExpiryRequired, run: func() error {
			_, err := fixture.tasks.ClaimQueued(t.Context(), nil)

			return fmt.Errorf("lease operation: %w", err)
		}},
		{name: "claim without owner", wantError: leaseOwnerExpiryRequired, run: func() error {
			_, err := fixture.tasks.ClaimQueued(t.Context(), &database.TaskClaim{LeaseExpiresAt: expires, TaskID: "",
				LeaseOwner: "", EventKind: ""})

			return fmt.Errorf("lease operation: %w", err)
		}},
		{name: "claim without event", wantError: eventKindRequired, run: func() error {
			_, err := fixture.tasks.ClaimQueued(t.Context(), &database.TaskClaim{LeaseExpiresAt: expires, TaskID: "",
				LeaseOwner: "worker", EventKind: ""})

			return fmt.Errorf("lease operation: %w", err)
		}},
		{name: "renew without owner", wantError: leaseOwnerExpiryRequired, run: func() error {
			_, err := fixture.tasks.RenewLease(t.Context(), task.ID, "", expires)

			return fmt.Errorf("lease operation: %w", err)
		}},
		{name: "nil recovery", wantError: "requires a terminal target", run: func() error {
			_, err := fixture.tasks.RecoverExpired(t.Context(), nil)

			return fmt.Errorf("lease operation: %w", err)
		}},
		{name: "nonterminal recovery", wantError: "requires a terminal target", run: func() error {
			_, err := fixture.tasks.RecoverExpired(t.Context(), &database.TaskRecovery{ExpiresBefore: time.Time{},
				Kind: "", EventKind: "", ErrorCode: "", ErrorMessage: "", PayloadJSON: "",
				TargetState: database.TaskRunning})

			return fmt.Errorf("lease operation: %w", err)
		}},
		{name: "recovery with invalid event", wantError: eventKindRequired, run: func() error {
			_, err := fixture.tasks.RecoverExpired(t.Context(), &database.TaskRecovery{ExpiresBefore: time.Time{},
				Kind: "", EventKind: "", ErrorCode: "", ErrorMessage: "", PayloadJSON: `{}`,
				TargetState: database.TaskInterrupted})

			return fmt.Errorf("lease operation: %w", err)
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.ErrorContains(t, test.run(), test.wantError)
		})
	}
}

func TestTaskRepositoryLeaseOperationsPropagateContextErrors(t *testing.T) {
	t.Parallel()

	fixture := newTaskTestFixture(t)
	created := fixture.createOwner(t.Context())
	canceled, cancel := context.WithCancel(t.Context())
	cancel()

	expires := time.Now().Add(time.Minute)
	tests := []struct {
		run  func() error
		name string
	}{
		{name: "claim", run: func() error {
			_, err := fixture.tasks.ClaimQueued(canceled, &database.TaskClaim{TaskID: created.ID, LeaseOwner: "worker",
				EventKind: taskStartedEvent, LeaseExpiresAt: expires})

			return fmt.Errorf("lease operation: %w", err)
		}},
		{name: "renew", run: func() error {
			_, err := fixture.tasks.RenewLease(canceled, created.ID, "worker", expires)

			return fmt.Errorf("lease operation: %w", err)
		}},
		{name: "recover", run: func() error {
			_, err := fixture.tasks.RecoverExpired(canceled, &database.TaskRecovery{Kind: database.TaskKindAgent,
				TargetState: database.TaskInterrupted, EventKind: taskInterruptedEvent, ErrorCode: "", ErrorMessage: "",
				PayloadJSON: `{}`, ExpiresBefore: expires})

			return fmt.Errorf("lease operation: %w", err)
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, test.run(), context.Canceled)
		})
	}
}
