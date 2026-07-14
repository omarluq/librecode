package agenttask_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/omarluq/librecode/internal/agenttask"
	"github.com/omarluq/librecode/internal/database"
)

const completedResult = "done"

type fakeRunner struct {
	err          error
	started      chan string
	release      chan struct{}
	eventRelease chan struct{}
	result       agenttask.Result
	once         sync.Once
}

func (runner *fakeRunner) Run(
	ctx context.Context,
	task *database.AgentTaskEntity,
	sink agenttask.EventSink,
) (agenttask.Result, error) {
	if runner.started != nil {
		runner.started <- task.Task.ID
	}

	if runner.eventRelease != nil {
		select {
		case <-ctx.Done():
			return runner.result, errors.Join(errors.New("event wait canceled"), ctx.Err())
		case <-runner.eventRelease:
		}
	}

	if err := sink(ctx, "tool_started", map[string]string{"tool": "read"}); err != nil {
		return agenttask.Result{Text: "", UsageJSON: ""}, err
	}

	if runner.release != nil {
		select {
		case <-ctx.Done():
			return runner.result, errors.Join(errors.New("runner canceled"), ctx.Err())
		case <-runner.release:
		}
	}

	return runner.result, runner.err
}

func (runner *fakeRunner) unblock() {
	if runner.release != nil {
		runner.once.Do(func() { close(runner.release) })
	}
}

func TestServiceSubmitRunsAndPersistsResult(t *testing.T) {
	t.Parallel()

	tasks, agentTasks, sessions := repositories(t)
	parent := createSession(t, sessions, "parent", "")
	child := createSession(t, sessions, "child", parent.ID)
	runner := &fakeRunner{
		result: agenttask.Result{Text: completedResult, UsageJSON: `{}`}, err: nil,
		started: make(chan string, 1), release: nil, eventRelease: nil, once: sync.Once{},
	}
	service := newService(t, tasks, agentTasks, runner)

	created, err := service.Submit(t.Context(), submitRequest(parent.ID, child.ID))
	require.NoError(t, err)

	completed, err := service.Await(t.Context(), created.Task.ID)
	require.NoError(t, err)
	assert.Equal(t, database.TaskSucceeded, completed.Task.State)
	assert.Equal(t, "done", completed.Task.Result)

	events, err := tasks.ListEvents(t.Context(), created.Task.ID, 0, 10)
	require.NoError(t, err)
	assert.Equal(t, []string{"task_queued", "task_started", "tool_started", "task_succeeded"}, eventKinds(events))
}

func TestServicePublishesPersistedEvents(t *testing.T) {
	t.Parallel()

	tasks, agentTasks, sessions := repositories(t)
	parent := createSession(t, sessions, "parent", "")
	child := createSession(t, sessions, "child", parent.ID)
	runner := &fakeRunner{
		result: agenttask.Result{Text: completedResult, UsageJSON: `{}`}, err: nil,
		started: make(chan string, 1), release: make(chan struct{}),
		eventRelease: make(chan struct{}), once: sync.Once{},
	}
	service := newService(t, tasks, agentTasks, runner)

	created, err := service.Submit(t.Context(), submitRequest(parent.ID, child.ID))
	require.NoError(t, err)
	require.Equal(t, created.Task.ID, <-runner.started)

	subscription := service.Subscribe(created.Task.ID)
	t.Cleanup(subscription.Cancel)
	close(runner.eventRelease)
	runner.unblock()

	var live database.TaskEventEntity

	require.Eventually(t, func() bool {
		select {
		case live = <-subscription.Events:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)

	assert.Equal(t, "tool_started", live.Event.Kind)

	replayed, err := service.Events(t.Context(), created.Task.ID, live.Sequence-1, 10)
	require.NoError(t, err)
	require.NotEmpty(t, replayed)
	assert.Equal(t, live.Sequence, replayed[0].Sequence)
}

func TestServiceCancelRunningTask(t *testing.T) {
	t.Parallel()

	tasks, agentTasks, sessions := repositories(t)
	parent := createSession(t, sessions, "parent", "")
	child := createSession(t, sessions, "child", parent.ID)
	runner := &fakeRunner{
		result: agenttask.Result{Text: "partial", UsageJSON: `{}`}, err: nil,
		started: make(chan string, 1), release: make(chan struct{}), eventRelease: nil, once: sync.Once{},
	}
	service := newService(t, tasks, agentTasks, runner)

	created, err := service.Submit(t.Context(), submitRequest(parent.ID, child.ID))
	require.NoError(t, err)
	require.Equal(t, created.Task.ID, <-runner.started)

	canceled, found, err := service.Cancel(t.Context(), parent.ID, created.Task.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Contains(t, []database.TaskState{database.TaskCanceling, database.TaskCanceled}, canceled.State)

	terminalTask, err := service.Await(t.Context(), created.Task.ID)
	require.NoError(t, err)
	assert.Equal(t, database.TaskCanceled, terminalTask.Task.State)
	assert.Equal(t, "partial", terminalTask.Task.Result)
}

type recoverySetup func(
	*testing.T,
	*database.TaskRepository,
	*database.AgentTaskRepository,
	string,
	string,
) string

func TestServiceRecoversQueuedAndInterruptedTasks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		prepare   recoverySetup
		wantState database.TaskState
	}{
		{
			name: "queued task resumes",
			prepare: func(
				t *testing.T,
				_ *database.TaskRepository,
				agentTasks *database.AgentTaskRepository,
				parent string,
				child string,
			) string {
				t.Helper()
				created, err := agentTasks.Create(t.Context(), agentTaskEntity(parent, child))
				require.NoError(t, err)

				return created.Task.ID
			},
			wantState: database.TaskSucceeded,
		},
		{
			name: "running task becomes interrupted",
			prepare: func(
				t *testing.T,
				tasks *database.TaskRepository,
				agentTasks *database.AgentTaskRepository,
				parent string,
				child string,
			) string {
				t.Helper()
				created, err := agentTasks.Create(t.Context(), agentTaskEntity(parent, child))
				require.NoError(t, err)
				changed, err := tasks.Transition(
					t.Context(), created.Task.ID, []database.TaskState{database.TaskQueued},
					database.TaskRunning, "task_started",
				)
				require.NoError(t, err)
				require.True(t, changed)

				return created.Task.ID
			},
			wantState: database.TaskInterrupted,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tasks, agentTasks, sessions := repositories(t)
			parent := createSession(t, sessions, "parent", "")
			child := createSession(t, sessions, "child", parent.ID)
			taskID := testCase.prepare(t, tasks, agentTasks, parent.ID, child.ID)
			runner := &fakeRunner{
				result: agenttask.Result{Text: "recovered", UsageJSON: `{"input":1}`},
				err:    nil, started: nil, release: nil, eventRelease: nil, once: sync.Once{},
			}
			service := newService(t, tasks, agentTasks, runner)

			task, err := service.Await(t.Context(), taskID)
			require.NoError(t, err)
			assert.Equal(t, testCase.wantState, task.Task.State)

			if testCase.wantState == database.TaskSucceeded {
				assert.JSONEq(t, `{"input":1}`, task.UsageJSON)
			}
		})
	}
}

func TestServiceEnforcesSessionConcurrency(t *testing.T) {
	t.Parallel()

	tasks, agentTasks, sessions := repositories(t)
	parent := createSession(t, sessions, "parent", "")
	firstChild := createSession(t, sessions, "first", parent.ID)
	secondChild := createSession(t, sessions, "second", parent.ID)
	runner := &fakeRunner{
		result: agenttask.Result{Text: completedResult, UsageJSON: `{}`}, err: nil,
		started: make(chan string, 2), release: make(chan struct{}), eventRelease: nil, once: sync.Once{},
	}
	service, err := agenttask.New(context.Background(), agenttask.Options{
		Tasks: tasks, AgentTasks: agentTasks, Runner: runner, Concurrency: 2,
		SessionConcurrency: 1, QueueCapacity: 4, Timeout: time.Minute,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		runner.unblock()
		require.NoError(t, service.Shutdown(context.Background()))
	})

	first, err := service.Submit(t.Context(), submitRequest(parent.ID, firstChild.ID))
	require.NoError(t, err)
	second, err := service.Submit(t.Context(), submitRequest(parent.ID, secondChild.ID))
	require.NoError(t, err)

	startedID := <-runner.started

	assert.Contains(t, []string{first.Task.ID, second.Task.ID}, startedID)
	assert.Never(t, func() bool {
		select {
		case <-runner.started:
			return true
		default:
			return false
		}
	}, 50*time.Millisecond, 5*time.Millisecond)

	runner.unblock()

	for _, taskID := range []string{first.Task.ID, second.Task.ID} {
		completed, awaitErr := service.Await(t.Context(), taskID)
		require.NoError(t, awaitErr)
		assert.Equal(t, database.TaskSucceeded, completed.Task.State)
	}
}

func TestServiceRejectsWorkWhenQueueIsFull(t *testing.T) {
	t.Parallel()

	tasks, agentTasks, sessions := repositories(t)
	parent := createSession(t, sessions, "parent", "")
	runner := &fakeRunner{
		result: agenttask.Result{Text: completedResult, UsageJSON: `{}`}, err: nil,
		started: make(chan string, 1), release: make(chan struct{}), eventRelease: nil, once: sync.Once{},
	}
	service, err := agenttask.New(context.Background(), agenttask.Options{
		Tasks: tasks, AgentTasks: agentTasks, Runner: runner, Concurrency: 1,
		SessionConcurrency: 1, QueueCapacity: 1, Timeout: time.Minute,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		runner.unblock()
		require.NoError(t, service.Shutdown(context.Background()))
	})

	firstChild := createSession(t, sessions, "first", parent.ID)
	_, err = service.Submit(t.Context(), submitRequest(parent.ID, firstChild.ID))
	require.NoError(t, err)
	require.NotEmpty(t, <-runner.started)

	secondChild := createSession(t, sessions, "second", parent.ID)
	_, err = service.Submit(t.Context(), submitRequest(parent.ID, secondChild.ID))
	require.NoError(t, err)

	thirdChild := createSession(t, sessions, "third", parent.ID)
	rejected, err := service.Submit(t.Context(), submitRequest(parent.ID, thirdChild.ID))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "queue is full")
	persisted, found, getErr := service.Get(t.Context(), rejected.Task.ID)
	require.NoError(t, getErr)
	require.True(t, found)
	assert.Equal(t, database.TaskFailed, persisted.Task.State)
	assert.Equal(t, "queue_full", persisted.Task.ErrorCode)
}

func TestServiceFailureAndOwnerScoping(t *testing.T) {
	t.Parallel()

	tasks, agentTasks, sessions := repositories(t)
	parent := createSession(t, sessions, "parent", "")
	child := createSession(t, sessions, "child", parent.ID)
	runner := &fakeRunner{
		result: agenttask.Result{Text: "", UsageJSON: `{}`}, err: errors.New("provider failed"),
		started: nil, release: nil, eventRelease: nil, once: sync.Once{},
	}
	service := newService(t, tasks, agentTasks, runner)

	created, err := service.Submit(t.Context(), submitRequest(parent.ID, child.ID))
	require.NoError(t, err)
	failed, err := service.Await(t.Context(), created.Task.ID)
	require.NoError(t, err)
	assert.Equal(t, database.TaskFailed, failed.Task.State)
	assert.Equal(t, "run_failed", failed.Task.ErrorCode)

	listed, err := service.List(t.Context(), parent.ID, 10)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, created.Task.ID, listed[0].ID)
}

func newService(
	t *testing.T,
	tasks *database.TaskRepository,
	agentTasks *database.AgentTaskRepository,
	runner agenttask.Runner,
) *agenttask.Service {
	t.Helper()

	service, err := agenttask.New(context.Background(), agenttask.Options{
		Tasks: tasks, AgentTasks: agentTasks, Runner: runner, Concurrency: 1,
		SessionConcurrency: 0, QueueCapacity: 0, Timeout: time.Minute,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		if fake, ok := runner.(*fakeRunner); ok {
			fake.unblock()
		}

		require.NoError(t, service.Shutdown(context.Background()))
	})

	return service
}

func repositories(
	t *testing.T,
) (*database.TaskRepository, *database.AgentTaskRepository, *database.SessionRepository) {
	t.Helper()

	connection, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	require.NoError(t, database.Migrate(t.Context(), connection))

	return database.NewTaskRepository(connection),
		database.NewAgentTaskRepository(connection),
		database.NewSessionRepository(connection)
}

func createSession(
	t *testing.T,
	repository *database.SessionRepository,
	name string,
	parent string,
) *database.SessionEntity {
	t.Helper()

	session, err := repository.CreateSession(t.Context(), t.TempDir(), name, parent)
	require.NoError(t, err)

	return session
}

func agentTaskEntity(parentSessionID, childSessionID string) *database.AgentTaskEntity {
	return &database.AgentTaskEntity{
		Task: database.TaskEntity{
			ID: "", Kind: "", ParentTaskID: "", OwnerSessionID: parentSessionID,
			ConcurrencyKey: parentSessionID, State: "", Result: "", ErrorCode: "",
			ErrorMessage: "", CreatedAt: time.Time{}, StartedAt: nil, FinishedAt: nil, UpdatedAt: time.Time{},
		},
		ChildSessionID: childSessionID, AgentName: "general", Prompt: "do work",
		Model: "", Provider: "", PolicyJSON: `{}`, UsageJSON: `{}`, Depth: 1,
	}
}

func submitRequest(parentSessionID, childSessionID string) *agenttask.SubmitRequest {
	return &agenttask.SubmitRequest{
		ParentTaskID: "", OwnerSessionID: parentSessionID, ChildSessionID: childSessionID,
		ConcurrencyKey: parentSessionID, AgentName: "general", Prompt: "do work",
		Model: "", Provider: "", PolicyJSON: `{}`, Depth: 1,
	}
}

func eventKinds(events []database.TaskEventEntity) []string {
	kinds := make([]string, len(events))
	for index := range events {
		kinds[index] = events[index].Event.Kind
	}

	return kinds
}
