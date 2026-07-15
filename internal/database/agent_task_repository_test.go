package database_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

func TestAgentTaskRepositoryLifecycle(t *testing.T) {
	t.Parallel()

	fixture := newTaskTestFixture(t)
	ctx, agents, tasks := t.Context(), fixture.agents, fixture.tasks
	parent, child := fixture.createAgentTaskSessions(ctx)

	created, err := agents.Create(ctx, newAgentTask(parent.ID, child.ID))
	require.NoError(t, err)
	assert.NotEmpty(t, created.Task.ID)
	assert.Equal(t, database.TaskQueued, created.Task.State)
	assert.Equal(t, "agent", created.Task.Kind)

	assertTaskTransition(t, tasks, &taskTransitionAssertion{
		Context: ctx, TaskID: created.Task.ID, From: []database.TaskState{database.TaskSucceeded},
		Target: database.TaskRunning, EventKind: taskStartedEvent, Want: false,
	})
	assertTaskTransition(t, tasks, &taskTransitionAssertion{
		Context: ctx, TaskID: created.Task.ID, From: []database.TaskState{database.TaskQueued},
		Target: database.TaskRunning, EventKind: taskStartedEvent, Want: true,
	})
	assertTaskTransition(t, tasks, &taskTransitionAssertion{
		Context: ctx, TaskID: created.Task.ID, From: []database.TaskState{database.TaskRunning},
		Target: database.TaskSucceeded, EventKind: taskSucceededEvent, Want: true,
	})
	assertTaskTransition(t, tasks, &taskTransitionAssertion{
		Context: ctx, TaskID: created.Task.ID, From: []database.TaskState{database.TaskRunning},
		Target: database.TaskFailed, EventKind: taskFailedEvent, Want: false,
	})

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
	assert.Equal(t, []string{"task_queued", taskStartedEvent, taskSucceededEvent}, eventKinds(events))
	assert.Equal(t, []int64{1, 2, 3}, eventSequences(events))
	assert.NotEmpty(t, events[0].Event.ID)
	assert.NotEmpty(t, events[1].Event.ID)
	assert.NotEmpty(t, events[2].Event.ID)
}

func TestAgentTaskRepositoryFinishBehavior(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		usage       string
		wantError   string
		from        []database.TaskState
		wantChanged bool
	}{
		{name: "records terminal outcome and usage", usage: `{"input_tokens":12}`,
			from: []database.TaskState{database.TaskQueued}, wantChanged: true, wantError: ""},
		{name: "stale transition leaves usage unchanged", usage: `{"input_tokens":12}`,
			from: []database.TaskState{database.TaskRunning}, wantError: "", wantChanged: false},
		{name: "rejects invalid usage", usage: `{`, from: []database.TaskState{database.TaskQueued},
			wantError: "usage_json must be valid JSON", wantChanged: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newTaskTestFixture(t)
			ctx, agents := t.Context(), fixture.agents
			parent, child := fixture.createAgentTaskSessions(ctx)
			created, err := agents.Create(ctx, newAgentTask(parent.ID, child.ID))
			require.NoError(t, err)

			changed, err := agents.Finish(ctx, new(agentSuccessFinish(created.Task.ID, test.from)), test.usage)
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, test.wantChanged, changed)

			loaded, found, err := agents.Get(ctx, created.Task.ID)
			require.NoError(t, err)
			require.True(t, found)

			if test.wantChanged {
				assert.Equal(t, database.TaskSucceeded, loaded.Task.State)
				assert.JSONEq(t, test.usage, loaded.UsageJSON)
			} else {
				assert.Equal(t, database.TaskQueued, loaded.Task.State)
				assert.JSONEq(t, `{}`, loaded.UsageJSON)
			}
		})
	}
}

func TestAgentTaskRepositoryCreateValidationAndDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(*database.AgentTaskEntity)
		wantError string
	}{
		{name: "defaults JSON", mutate: func(task *database.AgentTaskEntity) {
			task.PolicyJSON, task.UsageJSON = "",
				""
		}, wantError: ""},
		{name: "invalid owner",
			mutate:    func(task *database.AgentTaskEntity) { task.Task.OwnerSessionID = invalidID },
			wantError: "task.owner_session_id must be a UUIDv7"},
		{name: "invalid parent task",
			mutate:    func(task *database.AgentTaskEntity) { task.Task.ParentTaskID = invalidID },
			wantError: "task.parent_task_id must be a UUIDv7"},
		{name: "invalid child", mutate: func(task *database.AgentTaskEntity) { task.ChildSessionID = invalidID },
			wantError: "child_session_id must be a UUIDv7"},
		{name: "blank agent", mutate: func(task *database.AgentTaskEntity) { task.AgentName = " " },
			wantError: "agent_name is required"},
		{name: "blank prompt", mutate: func(task *database.AgentTaskEntity) { task.Prompt = " " },
			wantError: "prompt is required"},
		{name: "invalid depth", mutate: func(task *database.AgentTaskEntity) { task.Depth = 0 },
			wantError: "depth must be positive"},
		{name: "invalid policy", mutate: func(task *database.AgentTaskEntity) { task.PolicyJSON = "{" },
			wantError: "policy_json must be valid JSON"},
		{name: "invalid usage", mutate: func(task *database.AgentTaskEntity) { task.UsageJSON = "{" },
			wantError: "usage_json must be valid JSON"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newTaskTestFixture(t)
			ctx, agents := t.Context(), fixture.agents
			parent, child := fixture.createAgentTaskSessions(ctx)
			candidate := newAgentTask(parent.ID, child.ID)
			test.mutate(candidate)

			created, err := agents.Create(ctx, candidate)
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
				assert.Nil(t, created)

				return
			}

			require.NoError(t, err)
			assert.JSONEq(t, `{}`, created.PolicyJSON)
			assert.JSONEq(t, `{}`, created.UsageJSON)
		})
	}
}

func TestAgentTaskRepositoryPropagatesContextErrors(t *testing.T) {
	t.Parallel()

	fixture := newTaskTestFixture(t)
	ctx, agents := t.Context(), fixture.agents
	parent, child := fixture.createAgentTaskSessions(ctx)
	created, err := agents.Create(ctx, newAgentTask(parent.ID, child.ID))
	require.NoError(t, err)

	canceled, cancel := context.WithCancel(ctx)
	cancel()

	tests := []struct {
		run  func() error
		name string
	}{
		{name: "create", run: func() error {
			_, runErr := agents.Create(canceled, newAgentTask(parent.ID, child.ID))

			return fmt.Errorf("agent task operation: %w", runErr)
		}},
		{name: "finish", run: func() error {
			_, runErr := agents.Finish(canceled, new(newTaskFinish(created.Task.ID,
				[]database.TaskState{database.TaskQueued}, database.TaskSucceeded, taskSucceededEvent)), `{}`)

			return fmt.Errorf("agent task operation: %w", runErr)
		}},
		{name: "get", run: func() error {
			_, _, runErr := agents.Get(canceled, created.Task.ID)

			return fmt.Errorf("agent task operation: %w", runErr)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, test.run(), context.Canceled)
		})
	}
}

func TestAgentTaskRepositoryGetMissing(t *testing.T) {
	t.Parallel()

	fixture := newTaskTestFixture(t)
	entity, found, err := fixture.agents.Get(t.Context(), testUUIDV7(t))
	require.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, entity)
}

func TestSessionDeleteRemovesChildAgentTaskAndEvents(t *testing.T) {
	t.Parallel()

	fixture := newTaskTestFixture(t)
	ctx, agents, tasks, sessions := t.Context(), fixture.agents, fixture.tasks, fixture.sessions
	parent, child := fixture.createAgentTaskSessions(ctx)
	created, err := agents.Create(ctx, newAgentTask(parent.ID, child.ID))
	require.NoError(t, err)
	require.NoError(t, sessions.DeleteSession(ctx, child.ID))

	_, found, err := agents.Get(ctx, created.Task.ID)
	require.NoError(t, err)
	assert.False(t, found)
	_, found, err = tasks.Get(ctx, created.Task.ID)
	require.NoError(t, err)
	assert.False(t, found)

	events, err := tasks.ListEvents(ctx, created.Task.ID, 0, 10)
	require.NoError(t, err)
	assert.Empty(t, events)

	_, parentFound, err := sessions.GetSession(ctx, parent.ID)
	require.NoError(t, err)
	assert.True(t, parentFound)
}

func TestTaskRepositoryAppendsPolymorphicEvents(t *testing.T) {
	t.Parallel()

	fixture := newTaskTestFixture(t)
	ctx, agents, tasks := t.Context(), fixture.agents, fixture.tasks
	parent, child := fixture.createAgentTaskSessions(ctx)
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

type taskTransitionAssertion struct {
	Context   context.Context
	TaskID    string
	EventKind string
	Target    database.TaskState
	From      []database.TaskState
	Want      bool
}

func assertTaskTransition(
	t *testing.T,
	repository *database.TaskRepository,
	assertion *taskTransitionAssertion,
) {
	t.Helper()

	changed, err := repository.Transition(
		assertion.Context, assertion.TaskID, assertion.From, assertion.Target, assertion.EventKind,
	)
	require.NoError(t, err)
	assert.Equal(t, assertion.Want, changed)
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

func agentSuccessFinish(taskID string, from []database.TaskState) database.TaskFinish {
	finish := newTaskFinish(taskID, from, database.TaskSucceeded, taskSucceededEvent)
	finish.Result = "done"

	return finish
}
