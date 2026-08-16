package database_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

func TestTaskRepositoryListFiltersAndLimits(t *testing.T) {
	t.Parallel()

	fixture := newTaskTestFixture(t)
	ctx, tasks := t.Context(), fixture.tasks
	owner := fixture.createOwner(ctx)
	other := fixture.createOwner(ctx)

	create := func(kind, ownerID string) *database.TaskEntity {
		t.Helper()

		candidate := newTask(ownerID)
		candidate.Kind = kind
		created, err := tasks.Create(ctx, candidate)
		require.NoError(t, err)

		return created
	}

	agentQueued := create(database.TaskKindAgent, owner.ID)
	agentRunning := create(database.TaskKindAgent, owner.ID)
	unknownQueued := create("extension-job", owner.ID)
	_ = create(database.TaskKindTool, owner.ID)
	_ = create("extension-job", other.ID)

	changed, err := tasks.Transition(ctx, agentRunning.ID, []database.TaskState{database.TaskQueued},
		database.TaskRunning, taskStartedEvent, "{}")
	require.NoError(t, err)
	require.True(t, changed)

	filtered, err := tasks.ListOwned(ctx, owner.ID, []string{database.TaskKindAgent},
		[]database.TaskState{database.TaskQueued}, 101)
	require.NoError(t, err)
	assert.Equal(t, []string{agentQueued.ID}, taskIDs(filtered))

	limited, err := tasks.ListOwned(ctx, owner.ID, nil, nil, 1)
	require.NoError(t, err)
	assert.Len(t, limited, 1)

	defaultLimit, err := tasks.ListOwned(ctx, owner.ID, nil, nil, -1)
	require.NoError(t, err)
	assert.Len(t, defaultLimit, 4)

	unknown, err := tasks.ListQueuedExcluding(ctx, []string{database.TaskKindAgent, database.TaskKindTool}, 101)
	require.NoError(t, err)
	require.Len(t, unknown, 2)
	assert.Contains(t, taskIDs(unknown), unknownQueued.ID)

	allQueued, err := tasks.ListQueuedExcluding(ctx, nil, 0)
	require.NoError(t, err)
	assert.Len(t, allQueued, 4)
}

func TestTaskRepositoryClaimInterruptedAndRunningEvents(t *testing.T) {
	t.Parallel()

	fixture := newTaskTestFixture(t)
	ctx, tasks := t.Context(), fixture.tasks
	owner := fixture.createOwner(ctx)
	created, err := tasks.Create(ctx, newTask(owner.ID))
	require.NoError(t, err)

	changed, err := tasks.Transition(ctx, created.ID, []database.TaskState{database.TaskQueued},
		database.TaskInterrupted, taskInterruptedEvent, "{}")
	require.NoError(t, err)
	require.True(t, changed)

	expires := time.Now().Add(time.Minute)
	claimed, err := tasks.ClaimInterrupted(ctx, &database.TaskClaim{
		TaskID: created.ID, LeaseOwner: testWorker, LeaseExpiresAt: expires, EventKind: "task_resumed",
	})
	require.NoError(t, err)
	require.True(t, claimed)

	event, appended, err := tasks.AppendRunningEvent(ctx, created.ID, "other-worker", "progress", `{}`)
	require.NoError(t, err)
	assert.False(t, appended)
	assert.Nil(t, event)

	event, appended, err = tasks.AppendRunningEvent(ctx, created.ID, testWorker, "progress", `{"step":1}`)
	require.NoError(t, err)
	require.True(t, appended)
	require.NotNil(t, event)
	assert.Equal(t, "progress", event.Event.Kind)
	assert.Equal(t, int64(4), event.Sequence)

	changed, err = tasks.Transition(ctx, created.ID, []database.TaskState{database.TaskRunning},
		database.TaskCanceling, "task_canceling", "{}")
	require.NoError(t, err)
	require.True(t, changed)

	event, appended, err = tasks.AppendRunningEvent(ctx, created.ID, testWorker, "cancel_progress", `{}`)
	require.NoError(t, err)
	require.True(t, appended)
	assert.Equal(t, "cancel_progress", event.Event.Kind)

	invalid, appended, err := tasks.AppendRunningEvent(ctx, created.ID, testWorker, "progress", `{`)
	require.ErrorContains(t, err, "valid JSON")
	assert.False(t, appended)
	assert.Nil(t, invalid)
}

func TestTaskRepositoryRecoveryHandlesCancelingAndUnleasedTasks(t *testing.T) {
	t.Parallel()

	fixture := newTaskTestFixture(t)
	ctx, tasks := t.Context(), fixture.tasks
	owner := fixture.createOwner(ctx)

	createRunning := func() *database.TaskEntity {
		t.Helper()

		created, err := tasks.Create(ctx, newTask(owner.ID))
		require.NoError(t, err)
		changed, err := tasks.Transition(ctx, created.ID, []database.TaskState{database.TaskQueued},
			database.TaskRunning, taskStartedEvent, "{}")
		require.NoError(t, err)
		require.True(t, changed)

		return created
	}

	running := createRunning()
	canceling := createRunning()
	changed, err := tasks.Transition(ctx, canceling.ID, []database.TaskState{database.TaskRunning},
		database.TaskCanceling, "task_canceling", "{}")
	require.NoError(t, err)
	require.True(t, changed)

	queued, err := tasks.Create(ctx, newTask(owner.ID))
	require.NoError(t, err)

	recovery := &database.TaskRecovery{
		Kind: database.TaskKindAgent, TargetState: database.TaskFailed, EventKind: "task_recovered",
		ErrorCode: "worker_lost", ErrorMessage: "worker disappeared", PayloadJSON: `{"recovered":true}`,
		ExpiresBefore: time.Now(),
	}
	recovered, err := tasks.RecoverExpired(ctx, recovery)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{running.ID, canceling.ID}, recovered)

	for _, id := range recovered {
		loaded, found, getErr := tasks.Get(ctx, id)
		require.NoError(t, getErr)
		require.True(t, found)
		assert.Equal(t, database.TaskFailed, loaded.State)
		assert.Equal(t, "worker_lost", loaded.ErrorCode)
		assert.Equal(t, "worker disappeared", loaded.ErrorMessage)
		assert.NotNil(t, loaded.FinishedAt)
	}

	loaded, found, err := tasks.Get(ctx, queued.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, database.TaskQueued, loaded.State)

	recovered, err = tasks.RecoverExpired(ctx, recovery)
	require.NoError(t, err)
	assert.Empty(t, recovered)
}

func TestToolTaskRepositoryValidationBranches(t *testing.T) {
	t.Parallel()

	fixture := newToolTaskTestFixture(t)
	valid := func() *database.ToolTaskEntity {
		return newToolTask(fixture.owner.ID, fixture.owner.CWD, "validation-call")
	}

	tests := []struct {
		name      string
		mutate    func(*database.ToolTaskEntity) *database.ToolTaskEntity
		wantError string
	}{
		{name: "nil", mutate: func(*database.ToolTaskEntity) *database.ToolTaskEntity { return nil },
			wantError: "tool task is required"},
		{name: "missing target", mutate: func(task *database.ToolTaskEntity) *database.ToolTaskEntity {
			task.TargetName = " "

			return task
		}, wantError: "target, cwd, owner, invocation, and wrapper call are required"},
		{name: "missing cwd", mutate: func(task *database.ToolTaskEntity) *database.ToolTaskEntity {
			task.CWD = ""

			return task
		}, wantError: "target, cwd, owner, invocation, and wrapper call are required"},
		{name: "nonpositive timeout", mutate: func(task *database.ToolTaskEntity) *database.ToolTaskEntity {
			task.TimeoutSeconds = 0

			return task
		}, wantError: "timeout must be positive"},
		{name: "array arguments", mutate: func(task *database.ToolTaskEntity) *database.ToolTaskEntity {
			task.ArgumentsJSON = `[]`

			return task
		}, wantError: "arguments must be a bounded JSON object"},
		{name: "oversized arguments", mutate: func(task *database.ToolTaskEntity) *database.ToolTaskEntity {
			task.ArgumentsJSON = `{"value":"` + strings.Repeat("x", 256*1024) + `"}`

			return task
		}, wantError: "arguments must be a bounded JSON object"},
		{name: "invalid policy", mutate: func(task *database.ToolTaskEntity) *database.ToolTaskEntity {
			task.PolicyJSON = `[]`

			return task
		}, wantError: "policy must be a JSON object"},
		{name: "invalid definition", mutate: func(task *database.ToolTaskEntity) *database.ToolTaskEntity {
			task.DefinitionJSON = `null`

			return task
		}, wantError: "definition must be a JSON object"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			candidate := test.mutate(valid())
			created, err := fixture.repository.Create(t.Context(), candidate)
			require.ErrorContains(t, err, test.wantError)
			assert.Nil(t, created)
		})
	}

	withDefaultPolicy := valid()
	withDefaultPolicy.InvocationID = "default-policy"
	withDefaultPolicy.WrapperCallID = "default-policy"
	withDefaultPolicy.PolicyJSON = ""
	created, err := fixture.repository.Create(t.Context(), withDefaultPolicy)
	require.NoError(t, err)
	assert.Equal(t, `{}`, created.PolicyJSON)
}

func TestToolTaskRepositoryListCancelAndFinishValidation(t *testing.T) {
	t.Parallel()

	fixture := newToolTaskTestFixture(t)
	ctx, repository := t.Context(), fixture.repository

	first, err := repository.Create(ctx, newToolTask(fixture.owner.ID, fixture.owner.CWD, "first"))
	require.NoError(t, err)
	second, err := repository.Create(ctx, newToolTask(fixture.owner.ID, fixture.owner.CWD, "second"))
	require.NoError(t, err)

	claimed, err := repository.Tasks().ClaimQueued(ctx, &database.TaskClaim{TaskID: second.Task.ID,
		LeaseOwner: testWorker, LeaseExpiresAt: time.Now().Add(time.Minute), EventKind: taskStartedEvent})
	require.NoError(t, err)
	require.True(t, claimed)

	queued, err := repository.ListByOwner(ctx, fixture.owner.ID, []database.TaskState{database.TaskQueued}, 101)
	require.NoError(t, err)
	assert.Equal(t, []string{first.Task.ID}, toolTaskIDs(queued))
	all, err := repository.ListByOwner(ctx, fixture.owner.ID, nil, 0)
	require.NoError(t, err)
	assert.Len(t, all, 2)

	invalid, err := repository.ListByOwner(ctx, invalidID, nil, 1)
	require.ErrorContains(t, err, "must be a UUIDv7")
	assert.Nil(t, invalid)

	canceling, found, err := repository.Cancel(ctx, fixture.owner.ID, second.Task.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, database.TaskCanceling, canceling.Task.State)
	unchanged, found, err := repository.Cancel(ctx, fixture.owner.ID, second.Task.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, database.TaskCanceling, unchanged.Task.State)

	missing, found, err := repository.Cancel(ctx, fixture.owner.ID, testUUIDV7(t))
	require.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, missing)

	finish := newTaskFinish(second.Task.ID, []database.TaskState{database.TaskCanceling},
		database.TaskCanceled, "task_canceled")
	finish.LeaseOwner = testWorker
	changed, err := repository.Finish(ctx, &finish, `{`)
	require.ErrorContains(t, err, "outcome_json must be valid JSON")
	assert.False(t, changed)
	changed, err = repository.Finish(ctx, nil, `{}`)
	require.ErrorContains(t, err, "task finish is required")
	assert.False(t, changed)

	finish.TargetState = database.TaskRunning
	changed, err = repository.Finish(ctx, &finish, `{}`)
	require.ErrorContains(t, err, "terminal target")
	assert.False(t, changed)
}

func TestToolTaskRepositoryRecoveryHandlesCancelingAndSkipsLiveLease(t *testing.T) {
	t.Parallel()

	fixture := newToolTaskTestFixture(t)
	ctx, repository := t.Context(), fixture.repository
	createClaimed := func(invocation string, expiry time.Time) *database.ToolTaskEntity {
		t.Helper()

		created, err := repository.Create(ctx, newToolTask(fixture.owner.ID, fixture.owner.CWD, invocation))
		require.NoError(t, err)
		claimed, err := repository.Tasks().ClaimQueued(ctx, &database.TaskClaim{TaskID: created.Task.ID,
			LeaseOwner: invocation, LeaseExpiresAt: expiry, EventKind: taskStartedEvent})
		require.NoError(t, err)
		require.True(t, claimed)

		return created
	}

	expired := createClaimed("expired", time.Now().Add(-time.Minute))
	canceling := createClaimed("canceling", time.Now().Add(-time.Minute))
	changed, err := repository.Tasks().Transition(ctx, canceling.Task.ID, []database.TaskState{database.TaskRunning},
		database.TaskCanceling, "task_canceling", "{}")
	require.NoError(t, err)
	require.True(t, changed)

	live := createClaimed("live", time.Now().Add(time.Hour))

	require.NoError(t, repository.RecoverExpired(ctx, time.Now()))

	for _, id := range []string{expired.Task.ID, canceling.Task.ID} {
		loaded, found, getErr := repository.Get(ctx, id)
		require.NoError(t, getErr)
		require.True(t, found)
		assert.Equal(t, database.TaskInterrupted, loaded.Task.State)
		require.NotNil(t, loaded.OutcomeVersion)
		assert.Equal(t, 1, *loaded.OutcomeVersion)
		require.NotNil(t, loaded.OutcomeJSON)
		assert.Contains(t, *loaded.OutcomeJSON, "lease expired")
	}

	loaded, found, err := repository.Get(ctx, live.Task.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, database.TaskRunning, loaded.Task.State)
	assert.Nil(t, loaded.OutcomeJSON)
}

type toolTaskTestFixture struct {
	repository *database.ToolTaskRepository
	owner      *database.SessionEntity
}

func newToolTaskTestFixture(t *testing.T) *toolTaskTestFixture {
	t.Helper()
	connection := openTestSQLite(t, filepath.Join(t.TempDir(), "tool-task-branches.db"), 0)
	require.NoError(t, database.Migrate(t.Context(), connection))
	repository, err := database.NewToolTaskRepository(connection)
	require.NoError(t, err)
	sessions, err := database.NewSessionRepository(connection)
	require.NoError(t, err)
	owner, err := sessions.CreateSession(t.Context(), t.TempDir(), "owner", "")
	require.NoError(t, err)

	return &toolTaskTestFixture{repository: repository, owner: owner}
}

func toolTaskIDs(tasks []database.ToolTaskEntity) []string {
	ids := make([]string, len(tasks))
	for index := range tasks {
		ids[index] = tasks[index].Task.ID
	}

	return ids
}
