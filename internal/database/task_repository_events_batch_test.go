package database_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

const (
	batchTextDelta = "text_delta"
)

func TestTaskRepositoryAppendRunningEventsBatch(t *testing.T) {
	t.Parallel()

	fixture := newTaskTestFixture(t)
	ctx, tasks := t.Context(), fixture.tasks
	owner := fixture.createOwner(ctx)

	created, err := tasks.Create(ctx, newTask(owner.ID))
	require.NoError(t, err)

	expires := time.Now().Add(time.Minute)
	claimed, err := tasks.ClaimQueued(ctx, &database.TaskClaim{
		TaskID: created.ID, LeaseOwner: testWorker, LeaseExpiresAt: expires, EventKind: taskStartedEvent,
	})
	require.NoError(t, err)
	require.True(t, claimed)

	// Empty batches are admitted without writing.
	empty, appended, err := tasks.AppendRunningEvents(ctx, created.ID, testWorker, nil)
	require.NoError(t, err)
	assert.True(t, appended)
	assert.Empty(t, empty)

	// Wrong owner writes nothing.
	rejected, appended, err := tasks.AppendRunningEvents(ctx, created.ID, "other-worker", []database.TaskEventDraft{
		{Kind: batchTextDelta, PayloadJSON: `{}`},
	})
	require.NoError(t, err)
	assert.False(t, appended)
	assert.Empty(t, rejected)

	// Owned batches append contiguously in one transaction.
	batch := []database.TaskEventDraft{
		{Kind: batchTextDelta, PayloadJSON: `{"text":"a"}`},
		{Kind: batchTextDelta, PayloadJSON: `{"text":"b"}`},
		{Kind: "tool_result", PayloadJSON: `{"tool":"read"}`},
	}
	events, appended, err := tasks.AppendRunningEvents(ctx, created.ID, testWorker, batch)
	require.NoError(t, err)
	require.True(t, appended)
	require.Len(t, events, len(batch))

	for index, want := range batch {
		assert.Equal(t, int64(index+3), events[index].Sequence, "sequence must be contiguous")
		assert.Equal(t, want.Kind, events[index].Event.Kind)
		assert.Equal(t, want.PayloadJSON, events[index].Event.PayloadJSON)
		assert.Equal(t, created.ID, events[index].TaskID)
	}

	// Invalid payloads reject the whole batch before writing.
	invalid, appended, err := tasks.AppendRunningEvents(ctx, created.ID, testWorker, []database.TaskEventDraft{
		{Kind: batchTextDelta, PayloadJSON: `{"text":"c"}`},
		{Kind: batchTextDelta, PayloadJSON: `{`},
	})
	require.ErrorContains(t, err, "valid JSON")
	assert.False(t, appended)
	assert.Nil(t, invalid)

	// The rejected batch must not have partially applied.
	persisted, err := tasks.ListEvents(ctx, created.ID, 0, 100)
	require.NoError(t, err)
	assert.Len(t, persisted, len(batch)+2, "queued + started + batch only")
}

func TestTaskRepositoryAppendRunningEventsAfterLeaseLoss(t *testing.T) {
	t.Parallel()

	fixture := newTaskTestFixture(t)
	ctx, tasks := t.Context(), fixture.tasks
	owner := fixture.createOwner(ctx)

	created, err := tasks.Create(ctx, newTask(owner.ID))
	require.NoError(t, err)

	// Claim with an already-expired lease: ownership checks fail closed.
	claimed, err := tasks.ClaimQueued(ctx, &database.TaskClaim{
		TaskID: created.ID, LeaseOwner: testWorker,
		LeaseExpiresAt: time.Now().Add(-time.Minute), EventKind: taskStartedEvent,
	})
	require.NoError(t, err)
	require.True(t, claimed)

	events, appended, err := tasks.AppendRunningEvents(ctx, created.ID, testWorker, []database.TaskEventDraft{
		{Kind: batchTextDelta, PayloadJSON: `{}`},
	})
	require.NoError(t, err)
	assert.False(t, appended)
	assert.Empty(t, events)

	// A fresh lease for a new owner admits appends again.
	recovered, err := tasks.RecoverExpired(ctx, &database.TaskRecovery{
		Kind: database.TaskKindAgent, TargetState: database.TaskInterrupted,
		EventKind: taskInterruptedEvent, ErrorCode: soakRestartErrorCode, ErrorMessage: "expired",
		PayloadJSON: `{}`, ExpiresBefore: time.Now(),
	})
	require.NoError(t, err)
	require.Contains(t, recovered, created.ID)

	resumed, err := tasks.ClaimInterrupted(ctx, &database.TaskClaim{
		TaskID: created.ID, LeaseOwner: "new-owner",
		LeaseExpiresAt: time.Now().Add(time.Minute), EventKind: "task_resumed",
	})
	require.NoError(t, err)
	require.True(t, resumed)

	events, appended, err = tasks.AppendRunningEvents(ctx, created.ID, testWorker, []database.TaskEventDraft{
		{Kind: batchTextDelta, PayloadJSON: `{}`},
	})
	require.NoError(t, err)
	assert.False(t, appended)
	assert.Empty(t, events)

	// The new owner may still append.
	events, appended, err = tasks.AppendRunningEvents(ctx, created.ID, "new-owner", []database.TaskEventDraft{
		{Kind: batchTextDelta, PayloadJSON: `{}`},
	})
	require.NoError(t, err)
	assert.True(t, appended)
	assert.Len(t, events, 1)
}
