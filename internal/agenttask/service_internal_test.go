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

	"github.com/omarluq/librecode/internal/database"
)

const workerName = "worker"

func emptyService() *Service {
	return &Service{
		runner: nil, getTaskFn: nil, renewLeaseFn: nil, active: nil, subscribers: nil, agentTasks: nil, queue: nil,
		cancel: nil, done: nil, sessionSlots: nil, tasks: nil, logger: nil, leaseOwner: "", wg: sync.WaitGroup{},
		nextSubscriber: 0, timeout: 0, sessionConcurrency: 0, leaseDuration: 0,
		leaseHeartbeatInterval: 0, leaseRenewalRetryInterval: 0, leaseRenewalAttemptTimeout: 0,
		leaseRenewalAttempts: 0, mu: sync.Mutex{},
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

func (fixture serviceRepositoryFixture) createQueuedAgentTask(
	t *testing.T,
) (*database.SessionEntity, *database.AgentTaskEntity) {
	t.Helper()

	owner, err := fixture.sessions.CreateSession(t.Context(), t.TempDir(), "owner", "")
	require.NoError(t, err)
	child, err := fixture.sessions.CreateSession(t.Context(), t.TempDir(), "child", owner.ID)
	require.NoError(t, err)

	entity := emptyAgentTask()
	entity.Task.OwnerSessionID = owner.ID
	entity.Task.ConcurrencyKey = owner.ID
	entity.ChildSessionID = child.ID
	entity.AgentName = generalAgent
	entity.Prompt = workPrompt
	entity.PolicyJSON = `{}`
	entity.UsageJSON = `{}`
	entity.Depth = 1
	created, err := fixture.agentTasks.Create(t.Context(), entity)
	require.NoError(t, err)

	return owner, created
}

func serviceWithRepositories(tasks *database.TaskRepository, agentTasks *database.AgentTaskRepository) *Service {
	service := emptyService()
	service.tasks = tasks
	service.getTaskFn = tasks.Get
	service.agentTasks = agentTasks
	service.subscribers = make(map[string]map[uint64]chan database.TaskEventEntity)

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
	service.queue = make(chan string)

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

func TestServiceInternalDefaultsAndTimeoutParsing(t *testing.T) {
	t.Parallel()

	concurrency, sessionConcurrency, capacity, timeout := optionDefaults(Options{
		Tasks: nil, AgentTasks: nil, Runner: nil, Logger: nil, Concurrency: 7,
		SessionConcurrency: 3, QueueCapacity: 9, Timeout: time.Second,
	})
	assert.Equal(t, 7, concurrency)
	assert.Equal(t, 3, sessionConcurrency)
	assert.Equal(t, 9, capacity)
	assert.Equal(t, time.Second, timeout)
	assert.Zero(t, persistedTimeout(`not-json`))
	assert.Equal(t, 2*time.Second, persistedTimeout(`{"limits":{"timeout":2000000000}}`))
}

func TestServiceInternalRepositoryAndEventErrors(t *testing.T) {
	t.Parallel()

	connection, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	require.NoError(t, err)
	require.NoError(t, database.Migrate(t.Context(), connection))
	tasks := database.NewTaskRepository(connection)
	agentTasks := database.NewAgentTaskRepository(connection)
	require.NoError(t, connection.Close())

	service := serviceWithRepositories(tasks, agentTasks)

	_, _, err = service.Get(t.Context(), "id")
	require.ErrorContains(t, err, "get agent task")
	_, err = service.List(t.Context(), "owner", 1)
	require.ErrorContains(t, err, "list agent tasks")
	_, err = service.Events(t.Context(), "id", 0, 1)
	require.ErrorContains(t, err, "list task events")
	err = service.eventSink("id")(t.Context(), "event", make(chan int))
	require.ErrorContains(t, err, "marshal task event")
	err = service.eventSink("id")(t.Context(), "event", map[string]string{"ok": "yes"})
	require.ErrorContains(t, err, "append task event")
	owned, err := service.ownsTask(t.Context(), "owner", "id")
	assert.False(t, owned)
	require.ErrorContains(t, err, "get agent task")
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
	_, created := fixture.createQueuedAgentTask(t)
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
	child, err := sessions.CreateSession(t.Context(), t.TempDir(), "child", owner.ID)
	require.NoError(t, err)

	entity := emptyAgentTask()
	entity.Task.OwnerSessionID = owner.ID
	entity.Task.ConcurrencyKey = owner.ID
	entity.ChildSessionID = child.ID
	entity.AgentName = generalAgent
	entity.Prompt = workPrompt
	entity.PolicyJSON = `{}`
	entity.UsageJSON = `{}`
	entity.Depth = 1
	created, err := agentTasks.Create(t.Context(), entity)
	require.NoError(t, err)
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

	cancelingEntity := emptyAgentTask()
	cancelingEntity.Task.OwnerSessionID = ownerID
	cancelingEntity.Task.ConcurrencyKey = ownerID
	cancelingEntity.ChildSessionID = cancelingChild.ID
	cancelingEntity.AgentName = generalAgent
	cancelingEntity.Prompt = workPrompt
	cancelingEntity.PolicyJSON = `{}`
	cancelingEntity.UsageJSON = `{}`
	cancelingEntity.Depth = 1
	canceling, err := agentTasks.Create(t.Context(), cancelingEntity)
	require.NoError(t, err)

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
			_, created := fixture.createQueuedAgentTask(t)

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

func TestServiceInternalRequeueStopsWithContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	service := serviceWithQueue()
	service.requeue(ctx, "task")
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
	assert.EqualValues(t, 3, attempts.Load())
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
	service.renewLease(ctx, cancel, "task", done)

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		require.FailNow(t, "lease renewal did not cancel the run")
	}

	<-done
	assert.EqualValues(t, 3, attempts.Load())
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
		select {
		case <-renewed:
		case <-time.After(time.Second):
			require.FailNow(t, "long-running task stopped renewing its lease")
		}
	}

	cancel()
	<-done
	assert.GreaterOrEqual(t, renewals.Load(), int32(wantedRenewals))
}
