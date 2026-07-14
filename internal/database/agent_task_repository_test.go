package database_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

const taskStartedEvent = "task_started"

func TestAgentTaskRepositoryLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	agents, tasks, sessions := newTaskTestRepositories(t)
	parent, child := createAgentTaskSessions(ctx, t, sessions)

	created, err := agents.Create(ctx, newAgentTask(parent.ID, child.ID))
	require.NoError(t, err)
	assert.NotEmpty(t, created.Task.ID)
	assert.Equal(t, database.TaskQueued, created.Task.State)
	assert.Equal(t, "agent", created.Task.Kind)

	assertTaskTransition(
		ctx, t, tasks, created.Task.ID,
		[]database.TaskState{database.TaskSucceeded}, database.TaskRunning, taskStartedEvent, false,
	)
	assertTaskTransition(
		ctx, t, tasks, created.Task.ID,
		[]database.TaskState{database.TaskQueued}, database.TaskRunning, taskStartedEvent, true,
	)
	assertTaskTransition(
		ctx, t, tasks, created.Task.ID,
		[]database.TaskState{database.TaskRunning}, database.TaskSucceeded, "task_succeeded", true,
	)
	assertTaskTransition(
		ctx, t, tasks, created.Task.ID,
		[]database.TaskState{database.TaskRunning}, database.TaskFailed, "task_failed", false,
	)

	loaded, found, err := agents.Get(ctx, created.Task.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, database.TaskSucceeded, loaded.Task.State)
	assert.Equal(t, child.ID, loaded.ChildSessionID)
	require.NotNil(t, loaded.Task.StartedAt)
	require.NotNil(t, loaded.Task.FinishedAt)

	events, err := tasks.ListEvents(ctx, created.Task.ID, 0, 100)
	require.NoError(t, err)
	require.Len(t, events, 3)
	assert.Equal(t, []string{"task_queued", taskStartedEvent, "task_succeeded"}, eventKinds(events))
	assert.Equal(t, []int64{1, 2, 3}, eventSequences(events))
	assert.NotEmpty(t, events[0].Event.ID)
	assert.NotEmpty(t, events[1].Event.ID)
	assert.NotEmpty(t, events[2].Event.ID)
}

func TestTaskRepositoryAppendsPolymorphicEvents(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	agents, tasks, sessions := newTaskTestRepositories(t)
	parent, child := createAgentTaskSessions(ctx, t, sessions)
	created, err := agents.Create(ctx, newAgentTask(parent.ID, child.ID))
	require.NoError(t, err)

	appended, err := tasks.AppendEvent(ctx, created.Task.ID, "tool_started", `{"tool":"bash"}`)
	require.NoError(t, err)
	assert.Equal(t, int64(2), appended.Sequence)
	assert.Equal(t, "tool_started", appended.Event.Kind)
	assert.JSONEq(t, `{"tool":"bash"}`, appended.Event.PayloadJSON)

	events, err := tasks.ListEvents(ctx, created.Task.ID, 1, 10)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, appended.Event.ID, events[0].Event.ID)
}

func assertTaskTransition(
	ctx context.Context,
	t *testing.T,
	repository *database.TaskRepository,
	taskID string,
	from []database.TaskState,
	target database.TaskState,
	kind string,
	want bool,
) {
	t.Helper()

	changed, err := repository.Transition(ctx, taskID, from, target, kind)
	require.NoError(t, err)
	assert.Equal(t, want, changed)
}

func newTaskTestRepositories(
	t *testing.T,
) (*database.AgentTaskRepository, *database.TaskRepository, *database.SessionRepository) {
	t.Helper()

	connection := openTestSQLite(t, t.TempDir()+"/tasks.db", 0)
	require.NoError(t, database.Migrate(context.Background(), connection))

	return database.NewAgentTaskRepository(connection),
		database.NewTaskRepository(connection),
		database.NewSessionRepository(connection)
}

func newAgentTask(parentSessionID, childSessionID string) *database.AgentTaskEntity {
	return &database.AgentTaskEntity{
		Task: database.TaskEntity{
			CreatedAt: time.Time{}, StartedAt: nil, FinishedAt: nil, UpdatedAt: time.Time{},
			ID: "", Kind: "", ParentTaskID: "", OwnerSessionID: parentSessionID,
			ConcurrencyKey: "", State: "", Result: "", ErrorCode: "", ErrorMessage: "",
		},
		ChildSessionID: childSessionID,
		AgentName:      "general",
		Prompt:         "inspect the repository",
		Model:          "",
		Provider:       "",
		PolicyJSON:     "{}",
		UsageJSON:      "{}",
		Depth:          1,
	}
}

func createAgentTaskSessions(
	ctx context.Context,
	t *testing.T,
	sessions *database.SessionRepository,
) (parent, child *database.SessionEntity) {
	t.Helper()

	parent, err := sessions.CreateSession(ctx, t.TempDir(), "parent", "")
	require.NoError(t, err)
	child, err = sessions.CreateSession(ctx, parent.CWD, "child", parent.ID)
	require.NoError(t, err)

	return parent, child
}

func eventKinds(events []database.TaskEventEntity) []string {
	result := make([]string, len(events))
	for index := range events {
		result[index] = events[index].Event.Kind
	}

	return result
}

func eventSequences(events []database.TaskEventEntity) []int64 {
	result := make([]int64, len(events))
	for index := range events {
		result[index] = events[index].Sequence
	}

	return result
}
