package database_test

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

func TestToolTaskRepositoryCreateIdempotentFinishAndOwnerScope(t *testing.T) {
	t.Parallel()

	connection := openTestSQLite(t, filepath.Join(t.TempDir(), "tool-tasks.db"), 0)
	require.NoError(t, database.Migrate(t.Context(), connection))
	tasks, err := database.NewTaskRepository(connection)
	require.NoError(t, err)
	repository, err := database.NewToolTaskRepository(connection)
	require.NoError(t, err)
	sessions, err := database.NewSessionRepository(connection)
	require.NoError(t, err)
	owner, err := sessions.CreateSession(t.Context(), t.TempDir(), "owner", "")
	require.NoError(t, err)
	other, err := sessions.CreateSession(t.Context(), t.TempDir(), "other", "")
	require.NoError(t, err)

	candidate := newToolTask(owner.ID, t.TempDir(), "call-1")
	candidate.TargetName = "read"
	candidate.ArgumentsJSON = `{"path":"go.mod"}`
	created, err := repository.Create(t.Context(), candidate)
	require.NoError(t, err)
	duplicate, err := repository.Create(t.Context(), candidate)
	require.NoError(t, err)
	assert.Equal(t, created.Task.ID, duplicate.Task.ID)
	_, found, err := repository.GetOwned(t.Context(), other.ID, created.Task.ID)
	require.NoError(t, err)
	assert.False(t, found)

	// Provider call IDs are session-local. They must not collide across owners.
	otherCandidate := *candidate
	otherCandidate.OwnerSessionID = other.ID
	otherCandidate.CWD = other.CWD
	otherCreated, err := repository.Create(t.Context(), &otherCandidate)
	require.NoError(t, err)
	assert.NotEqual(t, created.Task.ID, otherCreated.Task.ID)

	claimed, err := tasks.ClaimQueued(t.Context(), &database.TaskClaim{
		TaskID: created.Task.ID, LeaseOwner: testWorker,
		LeaseExpiresAt: time.Now().Add(time.Minute), EventKind: taskStartedEvent,
	})
	require.NoError(t, err)
	require.True(t, claimed)

	outcome := `{"result":{"content":[],"details":{}},"is_error":false}`
	finish := newTaskFinish(created.Task.ID, []database.TaskState{database.TaskRunning}, database.TaskSucceeded,
		taskSucceededEvent)
	finish.Result, finish.LeaseOwner = testDone, testWorker
	finished, err := repository.Finish(t.Context(), &finish, outcome)
	require.NoError(t, err)
	require.True(t, finished)
	loaded, found, err := repository.GetOwned(t.Context(), owner.ID, created.Task.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, database.TaskSucceeded, loaded.Task.State)
	require.NotNil(t, loaded.OutcomeJSON)
	assert.JSONEq(t, outcome, *loaded.OutcomeJSON)
}

func TestToolTaskRepositoryCreateDefaultsPolicyWithoutMutatingCandidate(t *testing.T) {
	t.Parallel()

	fixture := newToolTaskTestFixture(t)
	candidate := newToolTask(fixture.owner.ID, fixture.owner.CWD, "default-policy")
	candidate.PolicyJSON = ""

	created, err := fixture.repository.Create(t.Context(), candidate)
	require.NoError(t, err)
	assert.Empty(t, candidate.PolicyJSON)
	assert.JSONEq(t, `{}`, created.PolicyJSON)
}

func TestToolTaskRepositoryFinishHonorsQueuedSourceState(t *testing.T) {
	t.Parallel()

	fixture := newToolTaskTestFixture(t)
	created, err := fixture.repository.Create(
		t.Context(), newToolTask(fixture.owner.ID, fixture.owner.CWD, "finish-queued"),
	)
	require.NoError(t, err)

	finish := newTaskFinish(
		created.Task.ID, []database.TaskState{database.TaskQueued}, database.TaskFailed, "task_failed",
	)
	finish.ErrorCode = "rejected"
	changed, err := fixture.repository.Finish(t.Context(), &finish, `{"is_error":true}`)
	require.NoError(t, err)
	require.True(t, changed)

	settled, found, err := fixture.repository.Get(t.Context(), created.Task.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, database.TaskFailed, settled.Task.State)
}

func TestToolTaskRepositoryCancelRacesClaimWithoutLosingCancellation(t *testing.T) {
	t.Parallel()

	connection := openTestSQLite(t, filepath.Join(t.TempDir(), "cancel-claim-race.db"), time.Second)
	require.NoError(t, database.Migrate(t.Context(), connection))
	repository, err := database.NewToolTaskRepository(connection)
	require.NoError(t, err)
	sessions, err := database.NewSessionRepository(connection)
	require.NoError(t, err)
	owner, err := sessions.CreateSession(t.Context(), t.TempDir(), "owner", "")
	require.NoError(t, err)

	for attempt := range 25 {
		created, createErr := repository.Create(
			t.Context(), newToolTask(owner.ID, t.TempDir(), fmt.Sprintf("cancel-race-%d", attempt)),
		)
		require.NoError(t, createErr)

		start := make(chan struct{})

		var wait sync.WaitGroup
		wait.Add(2)

		errors := make(chan error, 2)

		go func() {
			defer wait.Done()

			<-start

			_, claimErr := repository.Tasks().ClaimQueued(t.Context(), &database.TaskClaim{
				TaskID: created.Task.ID, LeaseOwner: testWorker,
				LeaseExpiresAt: time.Now().Add(time.Minute), EventKind: taskStartedEvent,
			})
			errors <- claimErr
		}()
		go func() {
			defer wait.Done()

			<-start

			_, _, cancelErr := repository.Cancel(t.Context(), owner.ID, created.Task.ID)
			errors <- cancelErr
		}()

		close(start)
		wait.Wait()
		close(errors)

		for raceErr := range errors {
			require.NoError(t, raceErr)
		}

		loaded, found, loadErr := repository.GetOwned(t.Context(), owner.ID, created.Task.ID)
		require.NoError(t, loadErr)
		require.True(t, found)
		assert.Contains(t, []database.TaskState{database.TaskCanceled, database.TaskCanceling}, loaded.Task.State)
	}
}

func TestToolTaskRepositoryCancelingWinsTerminalSettlement(t *testing.T) {
	t.Parallel()

	fixture := newToolTaskTestFixture(t)
	repository, owner := fixture.repository, fixture.owner
	created, err := repository.Create(t.Context(), newToolTask(owner.ID, owner.CWD, "cancel-running"))
	require.NoError(t, err)
	claimed, err := repository.Tasks().ClaimQueued(t.Context(), &database.TaskClaim{
		TaskID: created.Task.ID, LeaseOwner: testWorker,
		LeaseExpiresAt: time.Now().Add(time.Minute), EventKind: taskStartedEvent,
	})
	require.NoError(t, err)
	require.True(t, claimed)
	canceling, found, err := repository.Cancel(t.Context(), owner.ID, created.Task.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, database.TaskCanceling, canceling.Task.State)

	finish := newTaskFinish(created.Task.ID, []database.TaskState{
		database.TaskRunning, database.TaskCanceling,
	}, database.TaskSucceeded, taskSucceededEvent)
	finish.LeaseOwner = testWorker
	changed, err := repository.Finish(t.Context(), &finish, `{"is_error":false}`)
	require.NoError(t, err)
	require.True(t, changed)
	settled, found, err := repository.GetOwned(t.Context(), owner.ID, created.Task.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, database.TaskCanceled, settled.Task.State)
	require.NotNil(t, settled.OutcomeJSON)
	assert.Contains(t, *settled.OutcomeJSON, "task canceled")
}

func TestToolTaskRepositorySettlementRequiresLiveLease(t *testing.T) {
	t.Parallel()

	fixture := newToolTaskTestFixture(t)
	repository, owner := fixture.repository, fixture.owner
	created, err := repository.Create(t.Context(), newToolTask(owner.ID, owner.CWD, "expired-settle"))
	require.NoError(t, err)

	expired := time.Now().Add(-time.Minute)
	claimed, err := repository.Tasks().ClaimQueued(t.Context(), &database.TaskClaim{
		TaskID: created.Task.ID, LeaseOwner: testWorker, LeaseExpiresAt: expired, EventKind: taskStartedEvent,
	})
	require.NoError(t, err)
	require.True(t, claimed)

	finish := newTaskFinish(created.Task.ID, []database.TaskState{database.TaskRunning}, database.TaskSucceeded,
		taskSucceededEvent)
	finish.LeaseOwner = testWorker
	changed, err := repository.Finish(t.Context(), &finish, `{"is_error":false}`)
	require.NoError(t, err)
	assert.False(t, changed)
	loaded, found, err := repository.GetOwned(t.Context(), owner.ID, created.Task.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, database.TaskRunning, loaded.Task.State)
	assert.Nil(t, loaded.OutcomeJSON)
}

func TestToolTaskRepositoryRecoverExpiredStoresCanonicalOutcome(t *testing.T) {
	t.Parallel()

	fixture := newToolTaskTestFixture(t)
	repository, owner := fixture.repository, fixture.owner
	created, err := repository.Create(t.Context(), newToolTask(owner.ID, owner.CWD, "recover-1"))
	require.NoError(t, err)

	expiredAt := time.Now().Add(-time.Minute)
	claimed, err := repository.Tasks().ClaimQueued(t.Context(), &database.TaskClaim{
		TaskID: created.Task.ID, LeaseOwner: "dead-worker", LeaseExpiresAt: expiredAt,
		EventKind: taskStartedEvent,
	})
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, repository.RecoverExpired(t.Context(), time.Now()))

	recovered, found, err := repository.GetOwned(t.Context(), owner.ID, created.Task.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, database.TaskInterrupted, recovered.Task.State)
	require.NotNil(t, recovered.OutcomeJSON)
	assert.Contains(t, *recovered.OutcomeJSON, "lease expired")
}

func TestToolTaskRepositoryCancelQueuedStoresCanonicalOutcome(t *testing.T) {
	t.Parallel()

	fixture := newToolTaskTestFixture(t)
	repository, owner := fixture.repository, fixture.owner
	created, err := repository.Create(t.Context(), newToolTask(owner.ID, owner.CWD, "cancel-1"))
	require.NoError(t, err)
	canceled, found, err := repository.Cancel(t.Context(), owner.ID, created.Task.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, database.TaskCanceled, canceled.Task.State)
	require.NotNil(t, canceled.OutcomeJSON)
}

func TestToolTaskRepositoryCreateRollsBackWhenOwnerIsMissing(t *testing.T) {
	t.Parallel()

	connection := openTestSQLite(t, filepath.Join(t.TempDir(), "missing-owner.db"), 0)
	require.NoError(t, database.Migrate(t.Context(), connection))
	repository, err := database.NewToolTaskRepository(connection)
	require.NoError(t, err)

	missingOwner := testUUIDV7(t)
	created, err := repository.Create(t.Context(), newToolTask(missingOwner, t.TempDir(), "orphan"))
	require.ErrorContains(t, err, "create tool task")
	assert.Nil(t, created)

	genericTasks, err := repository.Tasks().ListOwned(t.Context(), missingOwner, nil, nil, 100)
	require.NoError(t, err)
	assert.Empty(t, genericTasks, "the generic task insert must roll back with the tool task insert")
	loaded, found, err := repository.GetByInvocation(t.Context(), missingOwner, "orphan")
	require.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, loaded)
}

func TestToolTaskRepositoryLookupValidationAndMissingFinish(t *testing.T) {
	t.Parallel()

	connection := openTestSQLite(t, filepath.Join(t.TempDir(), "lookup-validation.db"), 0)
	require.NoError(t, database.Migrate(t.Context(), connection))
	repository, err := database.NewToolTaskRepository(connection)
	require.NoError(t, err)

	_, found, err := repository.Get(t.Context(), invalidID)
	require.ErrorContains(t, err, "must be a UUIDv7")
	assert.False(t, found)
	_, found, err = repository.GetByInvocation(t.Context(), invalidID, "call")
	require.ErrorContains(t, err, "must be a UUIDv7")
	assert.False(t, found)
	_, found, err = repository.GetOwned(t.Context(), testUUIDV7(t), invalidID)
	require.ErrorContains(t, err, "must be a UUIDv7")
	assert.False(t, found)
	_, found, err = repository.GetOwned(t.Context(), invalidID, testUUIDV7(t))
	require.ErrorContains(t, err, "must be a UUIDv7")
	assert.False(t, found)

	finish := newTaskFinish(testUUIDV7(t), []database.TaskState{database.TaskRunning},
		database.TaskSucceeded, taskSucceededEvent)
	finish.LeaseOwner = testWorker
	changed, err := repository.Finish(t.Context(), &finish, `{}`)
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestToolTaskRepositoryCancelIsNoopAfterTerminalState(t *testing.T) {
	t.Parallel()

	connection := openTestSQLite(t, filepath.Join(t.TempDir(), "terminal-cancel.db"), 0)
	require.NoError(t, database.Migrate(t.Context(), connection))
	repository, err := database.NewToolTaskRepository(connection)
	require.NoError(t, err)
	sessions, err := database.NewSessionRepository(connection)
	require.NoError(t, err)
	owner, err := sessions.CreateSession(t.Context(), t.TempDir(), "owner", "")
	require.NoError(t, err)
	created, err := repository.Create(t.Context(), newToolTask(owner.ID, owner.CWD, "terminal"))
	require.NoError(t, err)

	canceled, found, err := repository.Cancel(t.Context(), owner.ID, created.Task.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, database.TaskCanceled, canceled.Task.State)
	originalOutcome := canceled.OutcomeJSON

	unchanged, found, err := repository.Cancel(t.Context(), owner.ID, created.Task.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, database.TaskCanceled, unchanged.Task.State)
	assert.Equal(t, originalOutcome, unchanged.OutcomeJSON)
}

func newToolTask(ownerID, cwd, invocationID string) *database.ToolTaskEntity {
	return &database.ToolTaskEntity{
		OutcomeVersion: nil, OutcomeJSON: nil, Task: *newTask(ownerID), WrapperCallID: invocationID,
		OwnerSessionID: ownerID, InvocationID: invocationID, CWD: cwd, ParentCallID: "",
		InitiatingEntryID: "", PolicyJSON: `{}`, DefinitionJSON: `{}`, ArgumentsJSON: `{}`, TargetName: "ls",
		SourceSequence: 0, TimeoutSeconds: 30,
	}
}
