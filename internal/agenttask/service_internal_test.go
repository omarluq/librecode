package agenttask

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite" // Register the SQLite database/sql driver used by these tests.

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
)

const (
	childSessionName = "child"
	workerName       = "worker"
)

func emptyService() *Service {
	return &Service{
		runner: nil, getTaskFn: nil, renewLeaseFn: nil, active: nil, subscribers: nil,
		agentTasks: nil, workflows: nil, queue: nil,
		cancel: nil, done: nil, sessionSlots: nil, tasks: nil, logger: nil, leaseOwner: "", wg: sync.WaitGroup{},
		nextSubscriber: 0, timeout: 0, sessionConcurrency: 0, leaseDuration: 0,
		leaseHeartbeatInterval: 0, leaseRenewalRetryInterval: 0, leaseRenewalAttemptTimeout: 0,
		leaseRenewalAttempts: 0, mu: sync.Mutex{},
	}
}

func receive[T any](t *testing.T, channel <-chan T, destination *T, timeoutMessage string) {
	t.Helper()

	select {
	case *destination = <-channel:
	case <-time.After(time.Second):
		require.FailNow(t, timeoutMessage)
	}
}

type serviceRepositoryFixture struct {
	tasks      *database.TaskRepository
	agentTasks *database.AgentTaskRepository
	sessions   *database.SessionRepository
}

func newServiceRepositoryFixture(t *testing.T) serviceRepositoryFixture {
	t.Helper()

	connection, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	require.NoError(t, database.Migrate(t.Context(), connection))

	return serviceRepositoryFixture{
		tasks:      database.NewTaskRepository(connection),
		agentTasks: database.NewAgentTaskRepository(connection),
		sessions:   database.NewSessionRepository(connection),
	}
}

func (fixture serviceRepositoryFixture) createQueuedAgentTask(t *testing.T) *database.AgentTaskEntity {
	t.Helper()

	owner, err := fixture.sessions.CreateSession(t.Context(), t.TempDir(), "owner", "")
	require.NoError(t, err)
	child, err := fixture.sessions.CreateSession(t.Context(), t.TempDir(), childSessionName, owner.ID)
	require.NoError(t, err)

	return createQueuedAgentTask(t, fixture.agentTasks, owner.ID, child.ID)
}

func createQueuedAgentTask(
	t *testing.T,
	repository *database.AgentTaskRepository,
	ownerID string,
	childSessionID string,
) *database.AgentTaskEntity {
	t.Helper()

	entity := emptyAgentTask()
	entity.Task.OwnerSessionID = ownerID
	entity.Task.ConcurrencyKey = ownerID
	entity.ChildSessionID = childSessionID
	entity.AgentName = generalAgent
	entity.Prompt = workPrompt
	entity.PolicyJSON = `{}`
	entity.UsageJSON = `{}`
	entity.Depth = 1
	created, err := repository.Create(t.Context(), entity)
	require.NoError(t, err)

	return created
}

func serviceWithRepositories(tasks *database.TaskRepository, agentTasks *database.AgentTaskRepository) *Service {
	service := emptyService()
	service.tasks = tasks
	service.getTaskFn = tasks.Get
	service.agentTasks = agentTasks
	service.subscribers = make(map[string]map[uint64]chan database.TaskEventEntity)

	return service
}

func serviceWithClosedRepositories(t *testing.T) *Service {
	t.Helper()

	connection, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	require.NoError(t, err)
	require.NoError(t, database.Migrate(t.Context(), connection))

	service := serviceWithRepositories(
		database.NewTaskRepository(connection),
		database.NewAgentTaskRepository(connection),
	)
	require.NoError(t, connection.Close())

	return service
}

func serviceWithCancel(cancel context.CancelFunc) *Service {
	service := emptyService()
	service.cancel = cancel
	service.subscribers = make(map[string]map[uint64]chan database.TaskEventEntity)

	return service
}

func serviceWithQueue() *Service {
	service := emptyService()
	service.queue = make(chan string, 1)

	return service
}

type countingRunner struct {
	calls atomic.Int32
}

func (runner *countingRunner) Run(
	context.Context,
	*database.AgentTaskEntity,
	EventSink,
) (Result, error) {
	runner.calls.Add(1)

	return Result{Text: "", UsageJSON: ""}, nil
}

func leaseRenewalService(
	logs *bytes.Buffer,
	renewLease func(context.Context, string, string, time.Time) (bool, error),
) *Service {
	service := emptyService()
	service.leaseOwner = workerName
	service.leaseDuration = time.Minute
	service.leaseRenewalRetryInterval = time.Millisecond
	service.leaseRenewalAttemptTimeout = time.Second
	service.leaseRenewalAttempts = 3
	service.logger = slog.New(slog.NewTextHandler(logs, nil))
	service.renewLeaseFn = renewLease

	return service
}

func TestServiceInternalOptionDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		options            *Options
		name               string
		concurrency        int
		sessionConcurrency int
		capacity           int
		timeout            time.Duration
	}{
		{
			name: "configured values",
			options: &Options{
				Tasks:              nil,
				AgentTasks:         nil,
				Workflows:          nil,
				Runner:             nil,
				Logger:             nil,
				Concurrency:        7,
				SessionConcurrency: 3,
				QueueCapacity:      9,
				Timeout:            time.Second,
			},
			concurrency:        7,
			sessionConcurrency: 3,
			capacity:           9,
			timeout:            time.Second,
		},
		{
			name: "defaults",
			options: &Options{
				Tasks: nil, AgentTasks: nil, Workflows: nil, Runner: nil, Logger: nil,
				Timeout: 0, Concurrency: 0, SessionConcurrency: 0, QueueCapacity: 0,
			},
			concurrency:        defaultConcurrency,
			sessionConcurrency: defaultSessionConcurrency,
			capacity:           defaultQueueCapacity,
			timeout:            defaultTimeout,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assertions := assert.New(t)

			concurrency, sessionConcurrency, capacity, timeout := optionDefaults(test.options)
			assertions.Equal(test.concurrency, concurrency)
			assertions.Equal(test.sessionConcurrency, sessionConcurrency)
			assertions.Equal(test.capacity, capacity)
			assertions.Equal(test.timeout, timeout)
		})
	}
}

func TestServiceInternalPersistedTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		policyJSON string
		want       time.Duration
	}{
		{name: "invalid policy", policyJSON: `not-json`, want: 0},
		{name: "configured timeout", policyJSON: `{"limits":{"timeout":2000000000}}`, want: 2 * time.Second},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.want, persistedTimeout(test.policyJSON))
		})
	}
}

func TestServiceInternalRepositoryErrors(t *testing.T) {
	t.Parallel()

	service := serviceWithClosedRepositories(t)
	tests := append(repositoryWriteErrorCases(service), repositoryReadErrorCases(service)...)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.ErrorContains(t, test.run(t), test.wantError)
		})
	}
}

type repositoryErrorCase struct {
	run       func(*testing.T) error
	name      string
	wantError string
}

func repositoryWriteErrorCases(service *Service) []repositoryErrorCase {
	return []repositoryErrorCase{
		{name: "submit task", wantError: "create agent task", run: func(t *testing.T) error {
			t.Helper()
			_, err := service.Submit(t.Context(), &SubmitRequest{
				ParentTaskID: "", OwnerSessionID: "", ChildSessionID: "", ConcurrencyKey: "", AgentName: "",
				Prompt: "", Model: "", Provider: "", PolicyJSON: "", Depth: 0,
			})

			return err
		}},
		{
			name:      "submit task with child session",
			wantError: "create agent task with child session",
			run: func(t *testing.T) error {
				t.Helper()
				_, err := service.SubmitAgentTask(t.Context(), &assistant.AgentTaskRequest{
					ParentTaskID: "", OwnerSessionID: "owner", ChildSessionID: "", ChildSessionCWD: t.TempDir(),
					ChildSessionName: childSessionName, AgentName: generalAgent, Prompt: workPrompt,
					Model: "", Provider: "", PolicyJSON: `{}`, ConcurrencyKey: "", NodeKey: "",
					InvocationIndex: 0, Depth: 0,
				})

				return err
			},
		},
		{name: "recover interrupted tasks", wantError: "recover expired tasks", run: func(t *testing.T) error {
			t.Helper()

			return service.recoverInterrupted(t.Context())
		}},
		{name: "enqueue recovered tasks", wantError: "list queued tasks", run: func(t *testing.T) error {
			t.Helper()

			return service.enqueueRecovered(t.Context(), t.Context())
		}},
	}
}

func repositoryReadErrorCases(service *Service) []repositoryErrorCase {
	return []repositoryErrorCase{
		{name: "get task", wantError: "get agent task", run: func(t *testing.T) error {
			t.Helper()
			_, _, err := service.Get(t.Context(), "id")

			return err
		}},
		{name: "list tasks", wantError: "list agent tasks", run: func(t *testing.T) error {
			t.Helper()
			_, err := service.List(t.Context(), "owner", 1)

			return err
		}},
		{name: "list events", wantError: "list task events", run: func(t *testing.T) error {
			t.Helper()
			_, err := service.Events(t.Context(), "id", 0, 1)

			return err
		}},
		{name: "append event", wantError: "append task event", run: func(t *testing.T) error {
			t.Helper()

			return service.eventSink("id")(t.Context(), "event", map[string]string{"ok": "yes"})
		}},
		{name: "check task ownership", wantError: "get agent task", run: func(t *testing.T) error {
			t.Helper()
			owned, err := service.ownsTask(t.Context(), "owner", "id")
			assert.False(t, owned)

			return err
		}},
	}
}

func TestServiceInternalEventMarshalError(t *testing.T) {
	t.Parallel()

	service := emptyService()
	err := service.eventSink("id")(t.Context(), "event", make(chan int))
	assert.ErrorContains(t, err, "marshal task event")
}

func TestServiceInternalTerminalUpdateErrorsAreLogged(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	service := serviceWithClosedRepositories(t)
	service.logger = slog.New(slog.NewTextHandler(&logs, nil))

	service.rejectQueuedTask("id", "rejected", "rejected")
	service.finish(
		t.Context(),
		"id",
		database.TaskFailed,
		"task_failed",
		Result{Text: "", UsageJSON: ""},
		"failed",
		"failed",
	)

	assertions := assert.New(t)
	assertions.Contains(logs.String(), "reject queued agent task")
	assertions.Contains(logs.String(), "finish agent task")
}

func TestServiceInternalSubmitAgentTaskCreatesChildSession(t *testing.T) {
	t.Parallel()
	assertions := assert.New(t)
	must := require.New(t)

	fixture := newServiceRepositoryFixture(t)
	owner, err := fixture.sessions.CreateSession(t.Context(), t.TempDir(), "owner", "")
	must.NoError(err)

	service := serviceWithRepositories(fixture.tasks, fixture.agentTasks)
	service.queue = make(chan string, 1)
	service.done = make(chan struct{})

	created, err := service.SubmitAgentTask(t.Context(), &assistant.AgentTaskRequest{
		ParentTaskID: "", OwnerSessionID: owner.ID, ChildSessionID: "", ChildSessionCWD: t.TempDir(),
		ChildSessionName: childSessionName, AgentName: generalAgent, Prompt: workPrompt,
		Model: "", Provider: "", PolicyJSON: `{}`, ConcurrencyKey: owner.ID, NodeKey: "",
		InvocationIndex: 0, Depth: 1,
	})
	must.NoError(err)
	assertions.NotEmpty(created.ChildSessionID)

	var queuedTaskID string
	receive(t, service.queue, &queuedTaskID, "timed out waiting for queued task")
	assertions.Equal(created.Task.ID, queuedTaskID)
}

func TestServiceInternalTerminalEventSurvivesFullSubscriberBuffer(t *testing.T) {
	t.Parallel()

	const taskID = "full-buffer-task"

	service := emptyService()
	service.subscribers = make(map[string]map[uint64]chan database.TaskEventEntity)

	subscription := service.Subscribe(taskID)
	defer subscription.Cancel()

	events := subscription.Events

	for sequence := int64(1); sequence <= eventBuffer; sequence++ {
		service.publish(&database.TaskEventEntity{
			TaskID: taskID, Sequence: sequence,
			Event: database.EventEntity{
				CreatedAt: time.Time{}, ID: "", Kind: "text_delta", PayloadJSON: "",
			},
		})
	}

	service.publish(&database.TaskEventEntity{
		TaskID: taskID, Sequence: eventBuffer + 1,
		Event: database.EventEntity{
			CreatedAt: time.Time{}, ID: "", Kind: "task_succeeded", PayloadJSON: "",
		},
	})

	received := make([]database.TaskEventEntity, eventBuffer)
	for index := range eventBuffer {
		receive(t, events, &received[index], "timed out waiting for task event")
	}

	assert.Equal(t, int64(2), received[0].Sequence, "oldest stream event should be evicted")
	assert.Equal(t, "task_succeeded", received[len(received)-1].Event.Kind)
}

func TestServiceInternalClosesAllSubscriptions(t *testing.T) {
	t.Parallel()

	first := make(chan database.TaskEventEntity)
	second := make(chan database.TaskEventEntity)
	service := emptyService()
	service.subscribers = map[string]map[uint64]chan database.TaskEventEntity{
		"task": {1: first, 2: second},
	}
	service.closeSubscriptions()

	_, firstOpen := <-first
	_, secondOpen := <-second

	assert.False(t, firstOpen)
	assert.False(t, secondOpen)
	assert.Empty(t, service.subscribers)
}

func TestServiceInternalFinalizeInterruptedRun(t *testing.T) {
	t.Parallel()

	fixture := newServiceRepositoryFixture(t)
	tasks, agentTasks := fixture.tasks, fixture.agentTasks
	created := fixture.createQueuedAgentTask(t)
	changed, err := tasks.Transition(
		t.Context(), created.Task.ID, []database.TaskState{database.TaskQueued}, database.TaskRunning, "started",
	)
	require.NoError(t, err)
	require.True(t, changed)

	service := serviceWithRepositories(tasks, agentTasks)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	service.finalizeRun(ctx, created.Task.ID, Result{Text: "partial", UsageJSON: ""}, context.Canceled)

	completed, found, err := agentTasks.Get(t.Context(), created.Task.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, database.TaskInterrupted, completed.Task.State)
	assert.Equal(t, "service_stopped", completed.Task.ErrorCode)
	assert.JSONEq(t, `{}`, completed.UsageJSON)
}

func TestServiceInternalCancelsQueuedTask(t *testing.T) {
	t.Parallel()

	fixture := newServiceRepositoryFixture(t)
	testQueuedTaskCancellation(t, fixture.tasks, fixture.agentTasks, fixture.sessions)
}

func testQueuedTaskCancellation(
	t *testing.T,
	tasks *database.TaskRepository,
	agentTasks *database.AgentTaskRepository,
	sessions *database.SessionRepository,
) {
	t.Helper()

	owner, err := sessions.CreateSession(t.Context(), t.TempDir(), "owner", "")
	require.NoError(t, err)
	child, err := sessions.CreateSession(t.Context(), t.TempDir(), childSessionName, owner.ID)
	require.NoError(t, err)

	created := createQueuedAgentTask(t, agentTasks, owner.ID, child.ID)
	otherChild, err := sessions.CreateSession(t.Context(), t.TempDir(), "other", owner.ID)
	require.NoError(t, err)

	done := make(chan struct{})
	close(done)

	stoppedService := serviceWithRepositories(tasks, agentTasks)
	stoppedService.queue = make(chan string)
	stoppedService.done = done
	stopped, err := stoppedService.Submit(t.Context(), &SubmitRequest{
		ParentTaskID: "", OwnerSessionID: owner.ID, ChildSessionID: otherChild.ID, ConcurrencyKey: owner.ID,
		AgentName: generalAgent, Prompt: workPrompt, Model: "", Provider: "", PolicyJSON: `{}`, Depth: 1,
	})
	require.ErrorContains(t, err, "enqueue task")
	require.NotNil(t, stopped)

	service := queuedService(tasks, agentTasks)
	service.publishLatest(t.Context(), "missing")
	service.run(t.Context(), "missing")
	awaitCtx, cancelAwait := context.WithCancel(t.Context())
	cancelAwait()

	_, err = service.Await(awaitCtx, created.Task.ID)
	require.ErrorContains(t, err, "await agent task")

	serviceCtx, cancelRecovery := context.WithCancel(t.Context())
	cancelRecovery()

	err = service.enqueueRecovered(t.Context(), serviceCtx)
	require.ErrorContains(t, err, "enqueue recovered tasks")

	testCancelingTaskFinalization(t, service, tasks, agentTasks, sessions, owner.ID)

	canceled, found, err := service.Cancel(t.Context(), owner.ID, created.Task.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, database.TaskCanceled, canceled.State)

	canceled, found, err = service.Cancel(t.Context(), owner.ID, created.Task.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, database.TaskCanceled, canceled.State)

	task, found, err := service.Cancel(t.Context(), "another-owner", created.Task.ID)
	require.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, task)
}
func queuedService(tasks *database.TaskRepository, agentTasks *database.AgentTaskRepository) *Service {
	service := serviceWithRepositories(tasks, agentTasks)
	service.queue = make(chan string)

	return service
}

func testCancelingTaskFinalization(
	t *testing.T,
	service *Service,
	tasks *database.TaskRepository,
	agentTasks *database.AgentTaskRepository,
	sessions *database.SessionRepository,
	ownerID string,
) {
	t.Helper()

	cancelingChild, err := sessions.CreateSession(t.Context(), t.TempDir(), "canceling", ownerID)
	require.NoError(t, err)

	canceling := createQueuedAgentTask(t, agentTasks, ownerID, cancelingChild.ID)

	changed, err := tasks.Transition(
		t.Context(), canceling.Task.ID, []database.TaskState{database.TaskQueued}, database.TaskRunning, "started",
	)
	require.NoError(t, err)
	require.True(t, changed)
	changed, err = tasks.Transition(
		t.Context(), canceling.Task.ID, []database.TaskState{database.TaskRunning}, database.TaskCanceling, "canceling",
	)
	require.NoError(t, err)
	require.True(t, changed)
	service.finalizeRun(t.Context(), canceling.Task.ID, Result{Text: "", UsageJSON: ""}, context.Canceled)
}

func TestServiceInternalSubmitRejectsBeforeWritableQueue(t *testing.T) {
	t.Parallel()

	const expectedOperation = "enqueue task"

	tests := []struct {
		name      string
		wantCode  string
		wantError string
		stop      bool
		cancel    bool
	}{
		{
			name: "canceled context", wantCode: "enqueue_canceled", wantError: expectedOperation,
			stop: false, cancel: true,
		},
		{
			name: "stopped service", wantCode: "service_stopped", wantError: expectedOperation,
			stop: true, cancel: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			fixture := newServiceRepositoryFixture(t)
			tasks, agentTasks := fixture.tasks, fixture.agentTasks
			created := fixture.createQueuedAgentTask(t)

			done := make(chan struct{})
			if test.stop {
				close(done)
			}

			service := serviceWithRepositories(tasks, agentTasks)
			service.queue = make(chan string, 1)
			service.done = done

			ctx, cancel := context.WithCancel(t.Context())
			if test.cancel {
				cancel()
			} else {
				t.Cleanup(cancel)
			}

			returned, err := service.enqueueCreated(ctx, created)
			require.ErrorContains(t, err, test.wantError)
			require.Same(t, created, returned)
			assert.Empty(t, service.queue)

			stored, found, err := agentTasks.Get(t.Context(), created.Task.ID)
			require.NoError(t, err)
			require.True(t, found)
			assert.Equal(t, database.TaskFailed, stored.Task.State)
			assert.Equal(t, test.wantCode, stored.Task.ErrorCode)
		})
	}
}

func TestServiceInternalExecuteSkipsUnavailableTask(t *testing.T) {
	t.Parallel()

	lookupErr := errors.New("lookup failed")
	tests := []struct {
		getTask  func(context.Context, string) (*database.TaskEntity, bool, error)
		wantErr  error
		name     string
		wantText string
	}{
		{
			name: "lookup error", wantErr: lookupErr, wantText: "",
			getTask: func(context.Context, string) (*database.TaskEntity, bool, error) {
				return nil, false, lookupErr
			},
		},
		{
			name: "missing task", wantErr: nil, wantText: "task not found before execution",
			getTask: func(context.Context, string) (*database.TaskEntity, bool, error) {
				return nil, false, nil
			},
		},
		{
			name: "canceling task", wantErr: context.Canceled, wantText: "",
			getTask: func(context.Context, string) (*database.TaskEntity, bool, error) {
				return &database.TaskEntity{
					ID: "", Kind: "", ParentTaskID: "", OwnerSessionID: "", ConcurrencyKey: "",
					State: database.TaskCanceling, Result: "", ErrorCode: "", ErrorMessage: "",
					LeaseOwner: "", CreatedAt: time.Time{}, StartedAt: nil, FinishedAt: nil,
					UpdatedAt: time.Time{}, LeaseExpiresAt: nil,
				}, true, nil
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			runner := new(countingRunner)
			service := emptyService()
			service.runner = runner
			service.getTaskFn = test.getTask
			service.active = make(map[string]context.CancelFunc)
			service.timeout = time.Minute
			service.leaseHeartbeatInterval = time.Minute

			_, err := service.execute(t.Context(), "task", emptyAgentTask())
			if test.wantErr != nil {
				require.ErrorIs(t, err, test.wantErr)
			} else {
				require.ErrorContains(t, err, test.wantText)
			}

			assert.Zero(t, runner.calls.Load())
			assert.NotContains(t, service.active, "task")
		})
	}
}

func TestServiceInternalShutdownCancellation(t *testing.T) {
	t.Parallel()

	serviceCtx, cancelService := context.WithCancel(t.Context())
	service := serviceWithCancel(cancelService)
	service.wg.Add(1)

	ctx, cancel := context.WithCancel(serviceCtx)
	cancel()

	err := service.Shutdown(ctx)
	require.ErrorContains(t, err, "shutdown task service")
	service.wg.Done()
}

func TestServiceInternalWorkerStopsBeforeQueuedWorkAfterCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	service := emptyService()

	service.queue = make(chan string, 1)

	service.queue <- "task"

	service.wg.Add(1)
	service.worker(ctx)

	assert.Len(t, service.queue, 1)
}

func TestServiceInternalRunIgnoresCanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	var logs bytes.Buffer

	service := emptyService()
	service.logger = slog.New(slog.NewTextHandler(&logs, nil))
	service.queue = make(chan string, 1)

	service.run(ctx, "task")

	assert.Empty(t, logs.String())
	assert.Empty(t, service.queue)
}

func TestServiceInternalHandleQueuedLoadError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		wantLog     string
		cancel      bool
		wantRequeue bool
	}{
		{name: "shutdown is ignored", cancel: true, wantRequeue: false, wantLog: ""},
		{name: "live service logs and requeues", cancel: false, wantRequeue: true, wantLog: "database unavailable"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assertions := assert.New(t)

			ctx, cancel := context.WithCancel(t.Context())
			if test.cancel {
				cancel()
			} else {
				t.Cleanup(cancel)
			}

			var logs bytes.Buffer

			service := serviceWithQueue()
			service.logger = slog.New(slog.NewTextHandler(&logs, nil))
			service.handleQueuedLoadError(ctx, "task", errors.New("database unavailable"))

			if test.wantRequeue {
				var taskID string
				receive(t, service.queue, &taskID, "task was not requeued")
				assertions.Equal("task", taskID)
			} else {
				assertions.Empty(service.queue)
			}

			if test.wantLog == "" {
				assertions.Empty(logs.String())
			} else {
				assertions.Contains(logs.String(), test.wantLog)
			}
		})
	}
}

func TestServiceInternalRunHandlesQueuedLoadError(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	service := serviceWithClosedRepositories(t)
	service.logger = slog.New(slog.NewTextHandler(&logs, nil))
	service.queue = make(chan string, 1)

	service.run(t.Context(), "task")

	var taskID string
	receive(t, service.queue, &taskID, "task was not requeued")
	assert.Equal(t, "task", taskID)

	assert.Contains(t, logs.String(), "load queued agent task")
}

func TestServiceInternalRequeueStopsWithContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	service := serviceWithQueue()
	service.requeue(ctx, "task")

	assert.Never(t, func() bool {
		return len(service.queue) > 0
	}, 5*dispatchRetryInterval, dispatchRetryInterval)
}

func TestServiceInternalLeaseRenewalRetriesTransientDatabaseErrors(t *testing.T) {
	t.Parallel()

	var (
		attempts atomic.Int32
		logs     bytes.Buffer
	)

	service := leaseRenewalService(&logs, func(context.Context, string, string, time.Time) (bool, error) {
		if attempts.Add(1) < 3 {
			return false, errors.New("database is locked")
		}

		return true, nil
	})

	assert.True(t, service.renewLeaseWithRetry(t.Context(), "task"))
	assert.Equal(t, int32(3), attempts.Load())
	assert.Contains(t, logs.String(), "retry agent task lease renewal")
	assert.NotContains(t, logs.String(), "renew agent task lease after retries")
}

func TestServiceInternalLeaseRenewalExhaustionCancelsLongRun(t *testing.T) {
	t.Parallel()

	var (
		attempts atomic.Int32
		logs     bytes.Buffer
	)

	service := leaseRenewalService(&logs, func(context.Context, string, string, time.Time) (bool, error) {
		attempts.Add(1)

		return false, errors.New("database is locked")
	})
	service.leaseHeartbeatInterval = time.Millisecond
	ctx, cancel := context.WithCancel(t.Context())

	done := make(chan struct{})
	go service.renewLease(ctx, cancel, "task", done)

	receive(t, ctx.Done(), new(struct{}), "lease renewal did not cancel the run")
	receive(t, done, new(struct{}), "timed out waiting for lease renewal to stop")
	assert.Equal(t, int32(3), attempts.Load())
	assert.Contains(t, logs.String(), "renew agent task lease after retries")
}

func TestServiceInternalLeaseRenewsThroughoutLongRun(t *testing.T) {
	t.Parallel()

	const wantedRenewals = 4

	var renewals atomic.Int32

	renewed := make(chan struct{}, wantedRenewals)
	service := leaseRenewalService(&bytes.Buffer{}, func(context.Context, string, string, time.Time) (bool, error) {
		renewals.Add(1)

		renewed <- struct{}{}

		return true, nil
	})
	service.leaseHeartbeatInterval = time.Millisecond
	ctx, cancel := context.WithCancel(t.Context())

	done := make(chan struct{})
	go service.renewLease(ctx, cancel, "task", done)

	for range wantedRenewals {
		receive(t, renewed, new(struct{}), "long-running task stopped renewing its lease")
	}

	cancel()
	receive(t, done, new(struct{}), "timed out waiting for lease renewal to stop")
	assert.GreaterOrEqual(t, renewals.Load(), int32(wantedRenewals))
}
