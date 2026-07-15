package database_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

func TestTaskRepositoryCreateGetAndList(t *testing.T) {
	t.Parallel()

	fixture := newTaskTestFixture(t)
	ctx, tasks := t.Context(), fixture.tasks
	owner := fixture.createOwner(t.Context())

	firstTask := newTask(owner.ID)
	firstTask.ConcurrencyKey = "one"
	first, err := tasks.Create(ctx, firstTask)
	require.NoError(t, err)

	secondTask := newTask(owner.ID)
	secondTask.ParentTaskID, secondTask.ConcurrencyKey = first.ID, "two"
	second, err := tasks.Create(ctx, secondTask)
	require.NoError(t, err)

	assert.Equal(t, database.TaskQueued, first.State)
	assert.False(t, first.CreatedAt.IsZero())
	assert.Equal(t, first.CreatedAt, first.UpdatedAt)

	loaded, found, err := tasks.Get(ctx, second.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, first.ID, loaded.ParentTaskID)
	assert.Equal(t, "two", loaded.ConcurrencyKey)

	_, found, err = tasks.Get(ctx, testUUIDV7(t))
	require.NoError(t, err)
	assert.False(t, found)

	byOwner, err := tasks.ListByOwner(ctx, database.TaskKindAgent, owner.ID, 0)
	require.NoError(t, err)
	require.Len(t, byOwner, 2)
	assert.Equal(t, second.ID, byOwner[0].ID)

	assertTaskIDs := func(t *testing.T, limit int, want []string) {
		t.Helper()

		listed, listErr := tasks.ListByStates(
			ctx, database.TaskKindAgent, []database.TaskState{database.TaskQueued}, limit,
		)
		require.NoError(t, listErr)
		assert.Equal(t, want, taskIDs(listed))
	}
	assertTaskIDs(t, 1, []string{first.ID})
	assertTaskIDs(t, 0, []string{first.ID, second.ID})

	empty, err := tasks.ListByStates(ctx, database.TaskKindAgent, nil, 10)
	require.NoError(t, err)
	assert.Nil(t, empty)
}

func TestTaskRepositoryCreateValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(*database.TaskEntity)
		wantError string
	}{
		{name: "blank kind", mutate: func(task *database.TaskEntity) { task.Kind = " " },
			wantError: "task.kind is required"},
		{name: "invalid owner", mutate: func(task *database.TaskEntity) { task.OwnerSessionID = invalidID },
			wantError: "task.owner_session_id must be a UUIDv7"},
		{name: "invalid parent", mutate: func(task *database.TaskEntity) { task.ParentTaskID = invalidID },
			wantError: "task.parent_task_id must be a UUIDv7"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newTaskTestFixture(t)
			ctx, tasks := t.Context(), fixture.tasks
			owner := fixture.createOwner(t.Context())
			candidate := newTask(owner.ID)
			test.mutate(candidate)

			created, err := tasks.Create(ctx, candidate)
			require.ErrorContains(t, err, test.wantError)
			assert.Nil(t, created)
		})
	}
}

func TestTaskRepositoryPropagatesContextErrors(t *testing.T) {
	t.Parallel()

	fixture := newTaskTestFixture(t)
	ctx, tasks := t.Context(), fixture.tasks
	owner := fixture.createOwner(t.Context())
	created, err := tasks.Create(ctx, newTask(owner.ID))
	require.NoError(t, err)

	canceled, cancel := context.WithCancel(ctx)
	cancel()

	tests := []struct {
		run  func() error
		name string
	}{
		{name: "create", run: func() error {
			_, runErr := tasks.Create(canceled, newTask(owner.ID))

			return fmt.Errorf("repository operation: %w", runErr)
		}},
		{name: "get", run: func() error {
			_, _, runErr := tasks.Get(canceled, created.ID)

			return fmt.Errorf("repository operation: %w", runErr)
		}},
		{name: "transition", run: func() error {
			_, runErr := tasks.Transition(canceled, created.ID, []database.TaskState{database.TaskQueued},
				database.TaskRunning, taskStartedEvent)

			return fmt.Errorf("repository operation: %w", runErr)
		}},
		{name: "finish", run: func() error {
			_, runErr := tasks.Finish(canceled, new(newTaskFinish(created.ID,
				[]database.TaskState{database.TaskQueued}, database.TaskFailed, taskFailedEvent)))

			return fmt.Errorf("repository operation: %w", runErr)
		}},
		{name: "list owner", run: func() error {
			_, runErr := tasks.ListByOwner(canceled, database.TaskKindAgent, owner.ID, 10)

			return fmt.Errorf("repository operation: %w", runErr)
		}},
		{name: "list states", run: func() error {
			_, runErr := tasks.ListByStates(
				canceled, database.TaskKindAgent, []database.TaskState{database.TaskQueued}, 10,
			)

			return fmt.Errorf("repository operation: %w", runErr)
		}},
		{name: "append event", run: func() error {
			_, runErr := tasks.AppendEvent(canceled, created.ID, "progress", `{}`)

			return fmt.Errorf("repository operation: %w", runErr)
		}},
		{name: "latest event", run: func() error {
			_, _, runErr := tasks.LatestEvent(canceled, created.ID)

			return fmt.Errorf("repository operation: %w", runErr)
		}},
		{name: "list events", run: func() error {
			_, runErr := tasks.ListEvents(canceled, created.ID, 0, 10)

			return fmt.Errorf("repository operation: %w", runErr)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, test.run(), context.Canceled)
		})
	}
}

func TestTaskRepositoryTransitionBehavior(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		target       database.TaskState
		event        string
		wantState    database.TaskState
		wantError    string
		from         []database.TaskState
		wantChanged  bool
		wantStarted  bool
		wantFinished bool
	}{
		{name: "starts queued task", from: []database.TaskState{database.TaskQueued}, target: database.TaskRunning,
			event: taskStartedEvent, wantChanged: true, wantState: database.TaskRunning, wantStarted: true,
			wantFinished: false, wantError: ""},
		{name: "ignores wrong source", from: []database.TaskState{database.TaskSucceeded},
			target: database.TaskRunning, event: taskStartedEvent, wantState: database.TaskQueued, wantError: "",
			wantChanged: false, wantStarted: false, wantFinished: false},
		{name: "finishes queued task", from: []database.TaskState{database.TaskQueued},
			target: database.TaskInterrupted, event: taskInterruptedEvent, wantChanged: true,
			wantState: database.TaskInterrupted, wantFinished: true, wantStarted: false, wantError: ""},
		{name: "rejects empty sources", target: database.TaskRunning, event: taskStartedEvent,
			wantState: database.TaskQueued, wantError: "requires a source state", from: nil, wantChanged: false,
			wantStarted: false, wantFinished: false},
		{name: "rejects blank event", from: []database.TaskState{database.TaskQueued}, target: database.TaskRunning,
			event: " ", wantState: database.TaskQueued, wantError: eventKindRequired, wantChanged: false,
			wantStarted: false, wantFinished: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newTaskTestFixture(t)
			ctx, tasks := t.Context(), fixture.tasks
			owner := fixture.createOwner(t.Context())
			created, err := tasks.Create(ctx, newTask(owner.ID))
			require.NoError(t, err)

			changed, err := tasks.Transition(ctx, created.ID, test.from, test.target, test.event)
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, test.wantChanged, changed)

			loaded, found, err := tasks.Get(ctx, created.ID)
			require.NoError(t, err)
			require.True(t, found)
			assert.Equal(t, test.wantState, loaded.State)
			assert.Equal(t, test.wantStarted, loaded.StartedAt != nil)
			assert.Equal(t, test.wantFinished, loaded.FinishedAt != nil)
		})
	}
}

func TestTaskRepositoryFinishBehavior(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		wantError   string
		finish      database.TaskFinish
		wantChanged bool
	}{
		{name: "succeeds", finish: successfulTaskFinish(), wantError: "", wantChanged: true},
		{name: "fails with details", finish: failedTaskFinish(), wantError: "", wantChanged: true},
		{name: "stale source", finish: newTaskFinish("", []database.TaskState{database.TaskRunning},
			database.TaskCanceled, "task_canceled"), wantError: "", wantChanged: false},
		{name: "non-terminal target", finish: newTaskFinish("", []database.TaskState{database.TaskQueued},
			database.TaskRunning, taskStartedEvent), wantError: "terminal target", wantChanged: false},
		{name: "missing sources", finish: newTaskFinish("", nil, database.TaskSucceeded, taskSucceededEvent),
			wantError: "source states", wantChanged: false},
		{name: "invalid payload", finish: invalidPayloadTaskFinish(), wantError: "valid JSON", wantChanged: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newTaskTestFixture(t)
			ctx, tasks := t.Context(), fixture.tasks
			owner := fixture.createOwner(t.Context())
			created, err := tasks.Create(ctx, newTask(owner.ID))
			require.NoError(t, err)

			test.finish.TaskID = created.ID

			changed, err := tasks.Finish(ctx, &test.finish)
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, test.wantChanged, changed)

			loaded, found, err := tasks.Get(ctx, created.ID)
			require.NoError(t, err)
			require.True(t, found)

			if test.wantChanged {
				assert.Equal(t, test.finish.TargetState, loaded.State)
				assert.Equal(t, test.finish.Result, loaded.Result)
				assert.Equal(t, test.finish.ErrorCode, loaded.ErrorCode)
				assert.Equal(t, test.finish.ErrorMessage, loaded.ErrorMessage)
				require.NotNil(t, loaded.FinishedAt)
			}
		})
	}
}

func TestTaskRepositoryEventBehavior(t *testing.T) {
	t.Parallel()

	fixture := newTaskTestFixture(t)
	ctx, tasks := t.Context(), fixture.tasks
	owner := fixture.createOwner(t.Context())
	created, err := tasks.Create(ctx, newTask(owner.ID))
	require.NoError(t, err)

	latest, found, err := tasks.LatestEvent(ctx, created.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "task_queued", latest.Event.Kind)
	assert.Equal(t, int64(1), latest.Sequence)

	appended, err := tasks.AppendEvent(ctx, created.ID, "progress", `{"percent":50}`)
	require.NoError(t, err)
	assert.Equal(t, int64(2), appended.Sequence)

	tests := []struct {
		name, kind, payload, wantError string
	}{
		{name: "rejects blank kind", kind: " ", payload: `{}`, wantError: eventKindRequired},
		{name: "rejects invalid payload", kind: "progress", payload: `{`, wantError: "valid JSON"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			invalidEvent, appendErr := tasks.AppendEvent(ctx, created.ID, test.kind, test.payload)
			require.ErrorContains(t, appendErr, test.wantError)
			assert.Nil(t, invalidEvent)
		})
	}

	events, err := tasks.ListEvents(ctx, created.ID, 1, 0)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, "progress", events[0].Event.Kind)

	_, found, err = tasks.LatestEvent(ctx, testUUIDV7(t))
	require.NoError(t, err)
	assert.False(t, found)
}

func taskIDs(tasks []database.TaskEntity) []string {
	ids := make([]string, len(tasks))
	for index := range tasks {
		ids[index] = tasks[index].ID
	}

	return ids
}

//go:fix inline

func successfulTaskFinish() database.TaskFinish {
	finish := newTaskFinish("", []database.TaskState{database.TaskQueued}, database.TaskSucceeded, taskSucceededEvent)
	finish.Result, finish.PayloadJSON = "done", `{"result":"done"}`

	return finish
}

func failedTaskFinish() database.TaskFinish {
	finish := newTaskFinish("", []database.TaskState{database.TaskQueued}, database.TaskFailed, taskFailedEvent)
	finish.ErrorCode, finish.ErrorMessage = "model", "failed"

	return finish
}

func invalidPayloadTaskFinish() database.TaskFinish {
	finish := newTaskFinish("", []database.TaskState{database.TaskQueued}, database.TaskSucceeded, taskSucceededEvent)
	finish.PayloadJSON = `{`

	return finish
}
