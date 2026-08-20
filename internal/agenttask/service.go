// Package agenttask owns durable asynchronous agent execution.
package agenttask

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
)

const (
	defaultConcurrency        = 4
	defaultSessionConcurrency = 2
	defaultTimeout            = 30 * time.Minute
	defaultQueueCapacity      = 256
	awaitPollInterval         = time.Second
	dispatchRetryInterval     = 10 * time.Millisecond
	finalizeTimeout           = 10 * time.Second
	leaseDuration             = 30 * time.Second
	leaseHeartbeatInterval    = 10 * time.Second
	leaseRenewalRetryInterval = 250 * time.Millisecond
	// leaseBusyGrace outlasts the database busy_timeout (15s default) so a
	// renewal attempt waits for the SQLite write lock instead of aborting while
	// the driver would still have succeeded.
	leaseBusyGrace     = 16 * time.Second
	eventBuffer        = 64
	eventFlushInterval = time.Second
	eventFlushBatch    = 32
	// cancelSourceEventLimit bounds the durable provenance scan; the
	// task_canceling event is among the task's most recent events.
	cancelSourceEventLimit = 64
	enqueueTaskOperation   = "enqueue task"
	enqueueCanceledCode    = "enqueue_canceled"
	enqueueCanceledMessage = "task submission was canceled before queue admission"
	serviceStoppedCode     = "service_stopped"
	serviceStoppedMessage  = "task service stopped before queue admission"
	taskInterruptedEvent   = "task_interrupted"
)

// errTaskTimeout marks a run ended by its execution timeout rather than an
// error in the run itself or an explicit cancellation.
var errTaskTimeout = errors.New("task timed out")

// Runner executes one persisted agent task.
type Runner interface {
	Run(context.Context, *database.AgentTaskEntity, EventSink) (Result, error)
}

// EventSink persists observable task progress.
type EventSink func(context.Context, string, any) error

// Subscription delivers best-effort live events. Durable replay remains available
// through Events when a subscriber falls behind.
type Subscription struct {
	Events <-chan database.TaskEventEntity
	Cancel func()
}

// Result is the terminal output of an agent run.
type Result struct {
	Text      string
	UsageJSON string
}

// SubmitRequest describes a durable agent task.
type SubmitRequest struct {
	ParentTaskID   string
	OwnerSessionID string
	ChildSessionID string
	ConcurrencyKey string
	AgentName      string
	Prompt         string
	Model          string
	Provider       string
	PolicyJSON     string
	Depth          int
}

// Options configures the task service.
type Options struct {
	Tasks      *database.TaskRepository
	AgentTasks *database.AgentTaskRepository
	Workflows  *database.WorkflowRepository
	Runner     Runner
	Logger     *slog.Logger
	Timeout    time.Duration

	Concurrency        int
	SessionConcurrency int
	QueueCapacity      int
}

// Service schedules and owns durable agent tasks.
type Service struct {
	runner                    Runner
	getTaskFn                 func(context.Context, string) (*database.TaskEntity, bool, error)
	awaitGetFn                func(context.Context, string) (*database.AgentTaskEntity, bool, error)
	renewLeaseFn              func(context.Context, string, string, time.Time) (bool, error)
	active                    map[string]context.CancelFunc
	cancelSources             map[string]string
	subscribers               map[string]map[uint64]chan database.TaskEventEntity
	sessionSlots              map[string]chan struct{}
	agentTasks                *database.AgentTaskRepository
	workflows                 *database.WorkflowRepository
	tasks                     *database.TaskRepository
	queue                     chan string
	cancel                    context.CancelFunc
	done                      <-chan struct{}
	logger                    *slog.Logger
	leaseOwner                string
	wg                        sync.WaitGroup
	mu                        sync.Mutex
	lifecycle                 sync.Mutex
	nextSubscriber            uint64
	awaitPollEvery            time.Duration
	timeout                   time.Duration
	leaseDuration             time.Duration
	leaseHeartbeatInterval    time.Duration
	leaseRenewalRetryInterval time.Duration
	concurrency               int
	sessionConcurrency        int
	started                   bool
	closed                    bool
}

func invalidOptions(options *Options) bool {
	return options == nil || options.Tasks == nil || options.AgentTasks == nil || options.Runner == nil
}

// New creates and starts a task service.
func New(ctx context.Context, options *Options) (*Service, error) {
	service, err := NewStopped(ctx, options)
	if err != nil {
		return nil, err
	}

	if err := service.Start(ctx); err != nil {
		return nil, err
	}

	return service, nil
}

// NewStopped creates a task service without starting its workers.
func NewStopped(ctx context.Context, options *Options) (*Service, error) {
	if invalidOptions(options) {
		return nil, errors.New("agenttask: tasks, agent tasks, and runner are required")
	}

	if ctx == nil {
		return nil, errors.New("agenttask: process context is required")
	}

	concurrency, sessionConcurrency, queueCapacity, timeout := optionDefaults(options)

	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}

	leaseOwner, err := newLeaseOwner()
	if err != nil {
		return nil, oops.In("agenttask").Code("worker_identity").Wrapf(err, "create worker identity")
	}

	service := &Service{
		runner: options.Runner, tasks: options.Tasks,
		agentTasks: options.AgentTasks, workflows: options.Workflows, queue: make(chan string, queueCapacity),
		cancel: nil, done: nil, active: make(map[string]context.CancelFunc),
		cancelSources:  make(map[string]string),
		sessionSlots:   make(map[string]chan struct{}),
		subscribers:    make(map[string]map[uint64]chan database.TaskEventEntity),
		nextSubscriber: 0, wg: sync.WaitGroup{}, timeout: timeout,
		concurrency: concurrency, sessionConcurrency: sessionConcurrency, logger: logger, leaseOwner: leaseOwner,
		getTaskFn: options.Tasks.Get, renewLeaseFn: options.Tasks.RenewLease, leaseDuration: leaseDuration,
		awaitGetFn:                options.AgentTasks.Get,
		awaitPollEvery:            awaitPollInterval,
		leaseHeartbeatInterval:    leaseHeartbeatInterval,
		leaseRenewalRetryInterval: leaseRenewalRetryInterval, started: false, closed: false,
		mu: sync.Mutex{}, lifecycle: sync.Mutex{},
	}
	if err := service.recoverInterrupted(ctx); err != nil {
		return nil, err
	}

	return service, nil
}

// Start launches workers and queues recovered tasks.
func (service *Service) Start(ctx context.Context) error {
	if ctx == nil {
		return oops.In("agenttask").Code("nil_start_context").Errorf("start context is required")
	}

	service.lifecycle.Lock()
	defer service.lifecycle.Unlock()

	if service.closed {
		return oops.In("agenttask").Code("service_stopped").Errorf("task service is stopped")
	}

	if service.started {
		return nil
	}

	workerCtx, cancel := context.WithCancel(ctx)
	service.cancel = cancel
	service.done = workerCtx.Done()

	for range service.concurrency {
		service.wg.Add(1)
		go service.worker(workerCtx)
	}

	if err := service.enqueueRecovered(ctx); err != nil {
		service.cancel()
		service.wg.Wait()
		service.closed = true
		service.closeSubscriptions()

		return err
	}

	service.started = true

	return nil
}

func optionDefaults(options *Options) (
	concurrency int,
	sessionConcurrency int,
	queueCapacity int,
	timeout time.Duration,
) {
	concurrency = options.Concurrency
	if concurrency <= 0 {
		concurrency = defaultConcurrency
	}

	sessionConcurrency = options.SessionConcurrency
	if sessionConcurrency <= 0 {
		sessionConcurrency = defaultSessionConcurrency
	}

	queueCapacity = options.QueueCapacity
	if queueCapacity <= 0 {
		queueCapacity = defaultQueueCapacity
	}

	timeout = options.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	return concurrency, sessionConcurrency, queueCapacity, timeout
}

// SubmitAgentTask adapts the assistant tool boundary to the durable scheduler.
func (service *Service) SubmitAgentTask(
	ctx context.Context,
	request *assistant.AgentTaskRequest,
) (*database.AgentTaskEntity, error) {
	submit := &SubmitRequest{
		ParentTaskID: request.ParentTaskID, OwnerSessionID: request.OwnerSessionID,
		ChildSessionID: request.ChildSessionID, ConcurrencyKey: request.ConcurrencyKey,
		AgentName: request.AgentName, Prompt: request.Prompt, Model: request.Model,
		Provider: request.Provider, PolicyJSON: request.PolicyJSON, Depth: request.Depth,
	}
	childRequest := &database.ChildSessionRequest{
		CWD: request.ChildSessionCWD, Name: request.ChildSessionName,
		ParentSessionID: request.OwnerSessionID,
	}

	var created *database.AgentTaskEntity

	var err error

	if request.ParentTaskID != "" && service.workflows == nil {
		return nil, oops.In("agenttask").Code("workflow_repository_missing").
			Errorf("workflow repository is required for workflow agent tasks")
	}

	switch {
	case request.ParentTaskID != "":
		created, err = service.workflows.CreateAgentTaskWithChildSession(
			ctx, request.ParentTaskID, agentTaskEntity(submit), childRequest,
			request.NodeKey, request.InvocationIndex,
		)
	case request.ChildSessionID == "":
		created, err = service.agentTasks.CreateWithChildSession(ctx, agentTaskEntity(submit), childRequest)
	default:
		return service.Submit(ctx, submit)
	}

	if err != nil {
		return nil, oops.In("agenttask").Code("create_agent_task").Wrapf(err, "create agent task with child session")
	}

	return service.enqueueCreated(ctx, created)
}

// Submit durably accepts a task before making it available to workers.
func (service *Service) Submit(ctx context.Context, request *SubmitRequest) (*database.AgentTaskEntity, error) {
	created, err := service.agentTasks.Create(ctx, agentTaskEntity(request))
	if err != nil {
		return nil, oops.In("agenttask").Code("create_task").Wrapf(err, "create agent task")
	}

	return service.enqueueCreated(ctx, created)
}

func agentTaskEntity(request *SubmitRequest) *database.AgentTaskEntity {
	return &database.AgentTaskEntity{
		Task: database.TaskEntity{
			ID: "", Kind: "", ParentTaskID: request.ParentTaskID,
			OwnerSessionID: request.OwnerSessionID, ConcurrencyKey: request.ConcurrencyKey,
			State: "", Result: "", ErrorCode: "", ErrorMessage: "", LeaseOwner: "",
			CreatedAt: time.Time{}, StartedAt: nil, FinishedAt: nil, UpdatedAt: time.Time{},
			LeaseExpiresAt: nil,
		},
		ChildSessionID: request.ChildSessionID, AgentName: request.AgentName,
		Prompt: request.Prompt, Model: request.Model, Provider: request.Provider,
		PolicyJSON: request.PolicyJSON, UsageJSON: "{}", Depth: request.Depth,
	}
}

func (service *Service) enqueueCreated(
	ctx context.Context,
	created *database.AgentTaskEntity,
) (*database.AgentTaskEntity, error) {
	if err := ctx.Err(); err != nil {
		return service.rejectCreatedTask(created, enqueueCanceledCode, enqueueCanceledMessage, err)
	}

	select {
	case <-service.done:
		return service.rejectCreatedTask(created, serviceStoppedCode, serviceStoppedMessage, context.Canceled)
	default:
	}

	select {
	case service.queue <- created.Task.ID:
		return created, nil
	case <-service.done:
		return service.rejectCreatedTask(created, serviceStoppedCode, serviceStoppedMessage, context.Canceled)
	case <-ctx.Done():
		return service.rejectCreatedTask(created, enqueueCanceledCode, enqueueCanceledMessage, ctx.Err())
	default:
		service.rejectQueuedTask(created.Task.ID, "queue_full", "agent task queue is full")

		return created, oops.In("agenttask").Code("queue_full").Errorf("agent task queue is full")
	}
}

func (service *Service) rejectCreatedTask(
	created *database.AgentTaskEntity,
	code string,
	message string,
	cause error,
) (*database.AgentTaskEntity, error) {
	service.rejectQueuedTask(created.Task.ID, code, message)

	return created, oops.In("agenttask").Code(code).Wrapf(cause, enqueueTaskOperation)
}

// Get returns an agent task by ID.
func (service *Service) Get(ctx context.Context, taskID string) (*database.AgentTaskEntity, bool, error) {
	task, found, err := service.agentTasks.Get(ctx, taskID)
	if err != nil {
		return nil, false, oops.In("agenttask").Code("get_task").Wrapf(err, "get agent task")
	}

	return task, found, nil
}

// List returns tasks owned by a session.
func (service *Service) List(
	ctx context.Context,
	ownerSessionID string,
	limit int,
) ([]database.AgentTaskEntity, error) {
	tasks, err := service.agentTasks.ListByOwner(ctx, ownerSessionID, limit)
	if err != nil {
		return nil, oops.In("agenttask").Code("list_tasks").Wrapf(err, "list agent tasks")
	}

	return tasks, nil
}

// Events returns durable task events after the requested sequence.
func (service *Service) Events(
	ctx context.Context,
	taskID string,
	after int64,
	limit int,
) ([]database.TaskEventEntity, error) {
	events, err := service.tasks.ListEvents(ctx, taskID, after, limit)
	if err != nil {
		return nil, oops.In("agenttask").Code("list_events").Wrapf(err, "list task events")
	}

	return events, nil
}

// Subscribe follows newly persisted events for a task. Delivery is bounded and
// best-effort; callers recover gaps using Events and the event sequence.
func (service *Service) Subscribe(taskID string) Subscription {
	subscription, err := service.subscribe(taskID)
	if err == nil {
		return subscription
	}

	channel := make(chan database.TaskEventEntity)
	close(channel)

	return Subscription{Events: channel, Cancel: func() {
		// The failed subscription owns no resources, so cancellation is a no-op.
	}}
}

func (service *Service) subscribe(taskID string) (Subscription, error) {
	service.lifecycle.Lock()
	defer service.lifecycle.Unlock()

	if service.closed {
		return Subscription{}, oops.In("agenttask").Code("service_stopped").Errorf("task service is stopped")
	}

	channel := make(chan database.TaskEventEntity, eventBuffer)

	service.mu.Lock()
	service.nextSubscriber++

	subscriberID := service.nextSubscriber
	if service.subscribers[taskID] == nil {
		service.subscribers[taskID] = make(map[uint64]chan database.TaskEventEntity)
	}

	service.subscribers[taskID][subscriberID] = channel
	service.mu.Unlock()

	var once sync.Once

	return Subscription{Events: channel, Cancel: func() {
		once.Do(func() {
			service.mu.Lock()
			subscribers := service.subscribers[taskID]

			registered, found := subscribers[subscriberID]
			if found {
				delete(subscribers, subscriberID)
				close(registered)
			}

			if len(subscribers) == 0 {
				delete(service.subscribers, taskID)
			}
			service.mu.Unlock()
		})
	}}, nil
}

// SubscribeAgentTask exposes task completion notifications through the assistant boundary.
func (service *Service) SubscribeAgentTask(
	taskID string,
) (events <-chan database.TaskEventEntity, cancel func(), err error) {
	subscription, err := service.subscribe(taskID)
	if err != nil {
		return nil, nil, err
	}

	return subscription.Events, subscription.Cancel, nil
}

// Cancel requests cancellation without allowing terminal states to change.
// The source records who requested cancellation for durable provenance.
func (service *Service) Cancel(
	ctx context.Context,
	ownerSessionID string,
	taskID string,
	source string,
) (*database.TaskEntity, bool, error) {
	owned, err := service.ownsTask(ctx, ownerSessionID, taskID)
	if err != nil || !owned {
		return nil, false, err
	}

	payload := database.CancelEventPayload(source)

	canceled, cancelErr := service.cancelQueued(ctx, taskID, payload)
	if cancelErr != nil {
		return nil, false, cancelErr
	}

	if canceled {
		service.publishLatest(ctx, taskID)
	} else {
		canceling, cancelErr := service.requestRunningCancel(ctx, taskID, payload)
		if cancelErr != nil {
			return nil, false, cancelErr
		}

		if canceling {
			service.rememberCancelSource(taskID, source)
			service.publishLatest(ctx, taskID)
			service.cancelActive(taskID)
		}
	}

	task, found, err := service.tasks.Get(ctx, taskID)
	if err != nil {
		return nil, false, oops.In("agenttask").Code("get_task").Wrapf(err, "get canceled task")
	}

	return task, found, nil
}

// cancelQueued moves a still-queued task straight to canceled. It reports false
// when the task is no longer queued so the caller can try the running path.
func (service *Service) cancelQueued(
	ctx context.Context, taskID, payload string,
) (bool, error) {
	changed, err := service.tasks.Transition(
		ctx, taskID, []database.TaskState{database.TaskQueued}, database.TaskCanceled, "task_canceled", payload,
	)
	if err != nil {
		return false, oops.In("agenttask").Code("cancel_task").Wrapf(err, "cancel queued task")
	}

	return changed, nil
}

// requestRunningCancel asks a running task to stop. It reports false when the
// task is not running.
func (service *Service) requestRunningCancel(
	ctx context.Context, taskID, payload string,
) (bool, error) {
	changed, err := service.tasks.Transition(
		ctx, taskID, []database.TaskState{database.TaskRunning}, database.TaskCanceling, "task_canceling", payload,
	)
	if err != nil {
		return false, oops.In("agenttask").Code("cancel_task").Wrapf(err, "cancel running task")
	}

	return changed, nil
}

// cancelActive signals the in-process run, if any, to stop.
func (service *Service) cancelActive(taskID string) {
	service.mu.Lock()
	cancel := service.active[taskID]
	service.mu.Unlock()

	if cancel != nil {
		cancel()
	}
}

func (service *Service) ownsTask(ctx context.Context, ownerSessionID, taskID string) (bool, error) {
	task, found, err := service.agentTasks.Get(ctx, taskID)
	if err != nil {
		return false, oops.In("agenttask").Code("get_task").Wrapf(err, "get agent task")
	}

	return found && task.Task.OwnerSessionID == ownerSessionID, nil
}

// Await waits until a task is terminal or the caller context ends.
func (service *Service) Await(ctx context.Context, taskID string) (*database.AgentTaskEntity, error) {
	subscription := service.Subscribe(taskID)
	defer subscription.Cancel()

	ticker := time.NewTicker(service.awaitPollEvery)
	defer ticker.Stop()

	events := subscription.Events

	for {
		task, found, err := service.awaitGetFn(ctx, taskID)
		if err != nil {
			return nil, oops.In("agenttask").Code("await_task").Wrapf(err, "await agent task")
		}

		if !found {
			return nil, fmt.Errorf("agenttask: task %q not found", taskID)
		}

		if terminal(task.Task.State) {
			return task, nil
		}

		select {
		case <-ctx.Done():
			return nil, oops.In("agenttask").Code("await_canceled").Wrapf(ctx.Err(), "await agent task")
		case _, open := <-events:
			if !open {
				events = nil
			}
		case <-ticker.C:
		}
	}
}

// Shutdown cancels active work and waits for all workers.
func (service *Service) Shutdown(ctx context.Context) error {
	service.lifecycle.Lock()
	if service.closed {
		service.lifecycle.Unlock()

		return nil
	}

	service.closed = true
	if service.cancel != nil {
		service.cancel()
	}

	service.closeSubscriptions()
	service.lifecycle.Unlock()

	done := make(chan struct{})

	go func() {
		service.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return oops.In("agenttask").Code("shutdown_canceled").Wrapf(ctx.Err(), "shutdown task service")
	}
}

func (service *Service) worker(ctx context.Context) {
	defer service.wg.Done()

	for {
		// Prefer shutdown over queued work when both cases are ready.
		select {
		case <-ctx.Done():
			return
		default:
		}

		select {
		case <-ctx.Done():
			return
		case taskID := <-service.queue:
			if ctx.Err() != nil {
				return
			}

			service.run(ctx, taskID)
		}
	}
}

func (service *Service) run(ctx context.Context, taskID string) {
	if ctx.Err() != nil {
		return
	}

	task, found, err := service.agentTasks.Get(ctx, taskID)
	if err != nil {
		service.handleQueuedLoadError(ctx, taskID, err)

		return
	}

	if !found {
		service.logWarn(ctx, "queued agent task not found", "task_id", taskID)

		return
	}

	release, acquired := service.acquireSessionSlot(task.Task.ConcurrencyKey)
	if !acquired {
		service.requeue(ctx, taskID)

		return
	}
	defer release()

	changed, err := service.tasks.ClaimQueued(ctx, &database.TaskClaim{
		TaskID: taskID, LeaseOwner: service.leaseOwner, EventKind: "task_started",
		LeaseExpiresAt: time.Now().Add(service.leaseDuration),
	})
	if err != nil {
		service.logError(ctx, "claim queued agent task", "task_id", taskID, "error", err)

		return
	}

	if !changed {
		return
	}

	service.publishLatest(ctx, taskID)

	task, found, err = service.agentTasks.Get(ctx, taskID)
	if err != nil {
		service.logError(ctx, "load claimed agent task", "task_id", taskID, "error", err)
	}

	if err != nil || !found {
		service.finish(
			ctx, taskID, database.TaskFailed, "task_failed",
			Result{Text: "", UsageJSON: ""}, "load_task", "load agent task",
		)

		return
	}

	result, runErr := service.execute(ctx, taskID, task)
	service.finalizeRun(ctx, taskID, result, runErr)
}

func (service *Service) handleQueuedLoadError(ctx context.Context, taskID string, err error) {
	// Cancellation can occur while the query is in flight. Shutdown is
	// expected and must not log or requeue durable work.
	if ctx.Err() != nil {
		return
	}

	service.logError(ctx, "load queued agent task", "task_id", taskID, "error", err)
	service.requeue(ctx, taskID)
}

func (service *Service) acquireSessionSlot(key string) (func(), bool) {
	service.mu.Lock()

	slots := service.sessionSlots[key]
	if slots == nil {
		slots = make(chan struct{}, service.sessionConcurrency)
		service.sessionSlots[key] = slots
	}
	service.mu.Unlock()

	select {
	case slots <- struct{}{}:
		return func() { <-slots }, true
	default:
		return func() {
			// No slot was acquired, so there is nothing to release.
		}, false
	}
}

func (service *Service) requeue(ctx context.Context, taskID string) {
	time.AfterFunc(dispatchRetryInterval, func() {
		if ctx.Err() != nil {
			return
		}

		select {
		case service.queue <- taskID:
		case <-ctx.Done():
		}
	})
}

func (service *Service) rejectQueuedTask(taskID, errorCode, errorMessage string) {
	ctx, cancel := context.WithTimeout(context.Background(), finalizeTimeout)
	defer cancel()

	payload, err := json.Marshal(map[string]string{"error_code": errorCode})
	if err != nil {
		service.logError(ctx, "marshal queued task rejection", "task_id", taskID, "error", err)

		return
	}

	changed, err := service.tasks.Finish(ctx, &database.TaskFinish{
		TaskID: taskID, From: []database.TaskState{database.TaskQueued},
		TargetState: database.TaskFailed, EventKind: "task_failed", Result: "",
		ErrorCode: errorCode, ErrorMessage: errorMessage, LeaseOwner: "",
		PayloadJSON: string(payload),
	})
	if err != nil {
		service.logError(ctx, "reject queued agent task", "task_id", taskID, "error", err)

		return
	}

	if changed {
		service.publishLatest(ctx, taskID)
	}
}

func (service *Service) execute(ctx context.Context, taskID string, task *database.AgentTaskEntity) (Result, error) {
	timeout := service.timeout
	if taskTimeout := persistedTimeout(task.PolicyJSON); taskTimeout > 0 {
		timeout = taskTimeout
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)

	heartbeatDone := make(chan struct{})
	go service.renewLease(runCtx, cancel, taskID, heartbeatDone)

	service.mu.Lock()
	service.active[taskID] = cancel
	service.mu.Unlock()

	defer func() {
		cancel()
		<-heartbeatDone

		service.mu.Lock()
		delete(service.active, taskID)
		service.mu.Unlock()
	}()

	// Cancellation can race with registration in active. Re-read durable state
	// after registration so a canceling task never starts model or tool work.
	current, found, err := service.getTaskFn(context.WithoutCancel(ctx), taskID)
	if err != nil {
		return Result{Text: "", UsageJSON: ""}, oops.In("agenttask").Code("get_task_state").Wrapf(
			err, "get task state before execution",
		)
	}

	if !found {
		return Result{Text: "", UsageJSON: ""}, oops.In("agenttask").Code("task_not_found").Errorf(
			"task not found before execution",
		)
	}

	if current.State == database.TaskCanceling {
		return Result{Text: "", UsageJSON: ""}, context.Canceled
	}

	writer := service.newTaskEventWriter(taskID)
	writer.start(runCtx)

	result, runErr := service.runner.Run(runCtx, task, writer.sink())
	if writeErr := writer.close(runCtx); writeErr != nil && runErr == nil {
		runErr = writeErr
	}

	// A run-context deadline with a live parent context is a timeout, not a
	// failure of the underlying run; record it distinctly.
	if errors.Is(runErr, context.DeadlineExceeded) && ctx.Err() == nil {
		runErr = fmt.Errorf("%w: %w", errTaskTimeout, runErr)
	}

	if runErr != nil {
		return result, oops.In("agenttask").Code("execute_task").Wrapf(runErr, "execute agent task")
	}

	return result, nil
}

func (service *Service) renewLease(
	ctx context.Context,
	cancel context.CancelFunc,
	taskID string,
	done chan<- struct{},
) {
	defer close(done)

	ticker := time.NewTicker(service.leaseHeartbeatInterval)
	defer ticker.Stop()

	validUntil := time.Now().Add(service.leaseDuration)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if service.renewLeaseWithRetry(ctx, taskID, validUntil) {
				validUntil = time.Now().Add(service.leaseDuration)

				continue
			}

			cancel()

			return
		}
	}
}

// renewLeaseWithRetry renews the lease, retrying transient database errors
// until the lease actually expires instead of an arbitrary retry window: a
// transient outage shorter than the lease must never forfeit it. SQLite waits
// up to its busy_timeout for the write lock, so each attempt gets
// min(busy grace, remaining lease validity) — a busy database keeps its chance
// to succeed while genuine lease loss (renewed=false) still fails fast.
func (service *Service) renewLeaseWithRetry(ctx context.Context, taskID string, deadline time.Time) bool {
	const busyGrace = leaseBusyGrace

	for attempt := 1; ; attempt++ {
		timeout := min(busyGrace, time.Until(deadline))
		if timeout <= 0 {
			service.logError(ctx, "renew agent task lease after lease expiry", "task_id", taskID,
				"lease_owner", service.leaseOwner, "attempts", attempt-1,
				"lease_duration", service.leaseDuration)

			return false
		}

		renewed, err := service.attemptLeaseRenewal(ctx, taskID, timeout)
		if err == nil {
			return service.handleLeaseRenewal(ctx, taskID, attempt, renewed)
		}

		if ctx.Err() != nil {
			return false
		}

		if !time.Now().Before(deadline) {
			service.logError(ctx, "renew agent task lease after lease expiry", "task_id", taskID,
				"lease_owner", service.leaseOwner, "attempts", attempt,
				"lease_duration", service.leaseDuration, "error", err)

			return false
		}

		service.logWarn(ctx, "retry agent task lease renewal", "task_id", taskID,
			"lease_owner", service.leaseOwner, "attempt", attempt,
			"lease_duration", service.leaseDuration,
			"retry_after", service.leaseRenewalRetryInterval, "error", err)

		retryDelay := min(service.leaseRenewalRetryInterval, time.Until(deadline))
		if !waitForLeaseRenewalRetry(ctx, retryDelay) {
			return false
		}
	}
}

func (service *Service) attemptLeaseRenewal(ctx context.Context, taskID string, timeout time.Duration) (bool, error) {
	attemptCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return service.renewLeaseFn(
		attemptCtx, taskID, service.leaseOwner, time.Now().Add(service.leaseDuration),
	)
}

func (service *Service) handleLeaseRenewal(
	ctx context.Context,
	taskID string,
	attempt int,
	renewed bool,
) bool {
	if !renewed {
		service.logWarn(ctx, "agent task lease ownership lost", "task_id", taskID,
			"lease_owner", service.leaseOwner)
	} else if attempt > 1 {
		service.logger.DebugContext(ctx, "agent task lease renewal recovered", "task_id", taskID,
			"lease_owner", service.leaseOwner, "attempt", attempt)
	}

	return renewed
}

func waitForLeaseRenewalRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (service *Service) finalizeRun(ctx context.Context, taskID string, result Result, runErr error) {
	current, found, err := service.tasks.Get(context.WithoutCancel(ctx), taskID)
	if err == nil && found && current.State == database.TaskCanceling {
		message := service.cancelSourceMessage(ctx, taskID)
		if message == "" && runErr != nil {
			message = runErr.Error()
		}

		service.forgetCancelSource(taskID)
		service.finish(ctx, taskID, database.TaskCanceled, "task_canceled", result, "canceled", message)

		return
	}

	if runErr == nil {
		service.finish(ctx, taskID, database.TaskSucceeded, "task_succeeded", result, "", "")

		return
	}

	if ctx.Err() != nil {
		service.finish(
			ctx, taskID, database.TaskInterrupted, taskInterruptedEvent, result,
			"service_stopped", "task interrupted by service shutdown",
		)

		return
	}

	if errors.Is(runErr, errTaskTimeout) {
		service.finish(ctx, taskID, database.TaskFailed, "task_failed", result, "timeout", runErr.Error())

		return
	}

	service.finish(ctx, taskID, database.TaskFailed, "task_failed", result, "run_failed", runErr.Error())
}

func (service *Service) publish(event *database.TaskEventEntity) {
	service.mu.Lock()
	defer service.mu.Unlock()

	terminal := isTerminalEventKind(event.Event.Kind)
	for _, subscriber := range service.subscribers[event.TaskID] {
		select {
		case subscriber <- *event:
		default:
			if !terminal {
				continue
			}

			// Preserve terminal state even when stream deltas have filled the
			// best-effort subscriber buffer. Evicting one older event is safe:
			// durable replay repairs the resulting sequence gap.
			select {
			case <-subscriber:
			default:
			}

			subscriber <- *event
		}
	}
}

func isTerminalEventKind(kind string) bool {
	switch kind {
	case "task_succeeded", "task_failed", "task_canceled", taskInterruptedEvent:
		return true
	default:
		return false
	}
}

func (service *Service) closeSubscriptions() {
	service.mu.Lock()
	defer service.mu.Unlock()

	for taskID, subscribers := range service.subscribers {
		for subscriberID, subscriber := range subscribers {
			close(subscriber)
			delete(subscribers, subscriberID)
		}

		delete(service.subscribers, taskID)
	}
}

// rememberCancelSource caches who requested cancellation. The durable
// task_canceling event payload is the source of truth; the cache only avoids
// a database read when this process recorded the request itself.
func (service *Service) rememberCancelSource(taskID, source string) {
	service.mu.Lock()
	defer service.mu.Unlock()

	service.cancelSources[taskID] = source
}

func (service *Service) forgetCancelSource(taskID string) {
	service.mu.Lock()
	defer service.mu.Unlock()

	delete(service.cancelSources, taskID)
}

// cancelSourceMessage describes the cancellation requester, or "" when no
// provenance is known. The in-memory cache covers cancels requested by this
// process; the durable task_canceling event payload also covers requests
// recorded by another process before this worker finalizes the run.
func (service *Service) cancelSourceMessage(ctx context.Context, taskID string) string {
	if source := service.cachedCancelSource(taskID); source != "" {
		return "task canceled by " + source
	}

	source := service.durableCancelSource(ctx, taskID)
	if source == "" {
		return ""
	}

	return "task canceled by " + source
}

func (service *Service) cachedCancelSource(taskID string) string {
	service.mu.Lock()
	defer service.mu.Unlock()

	return service.cancelSources[taskID]
}

// durableCancelSource reads the canceled_by payload from the task's most
// recent task_canceling event.
func (service *Service) durableCancelSource(ctx context.Context, taskID string) string {
	events, err := service.tasks.ListEvents(context.WithoutCancel(ctx), taskID, 0, cancelSourceEventLimit)
	if err != nil {
		// Provenance is best-effort; the event read may fail while the database
		// is busy finalizing the run. The caller falls back to the raw error.
		service.logError(ctx, "read cancel provenance", "task_id", taskID, "error", err)

		return ""
	}

	for _, event := range slices.Backward(events) {
		if event.Event.Kind != "task_canceling" {
			continue
		}

		payload := map[string]string{}
		if err := json.Unmarshal([]byte(event.Event.PayloadJSON), &payload); err != nil {
			continue
		}

		if source := payload[database.CanceledByJSONKey]; source != "" {
			return source
		}
	}

	return ""
}

func (service *Service) finish(
	serviceCtx context.Context,
	taskID string,
	state database.TaskState,
	kind string,
	result Result,
	errorCode string,
	errorMessage string,
) {
	payload, err := json.Marshal(map[string]string{"error_code": errorCode})
	if err != nil {
		service.logError(serviceCtx, "marshal agent task outcome", "task_id", taskID, "error", err)

		return
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(serviceCtx), finalizeTimeout)
	defer cancel()

	usageJSON := result.UsageJSON
	if usageJSON == "" {
		usageJSON = "{}"
	}

	changed, err := service.agentTasks.Finish(ctx, &database.TaskFinish{
		TaskID: taskID, From: []database.TaskState{database.TaskRunning, database.TaskCanceling},
		TargetState: state, EventKind: kind, Result: result.Text, LeaseOwner: service.leaseOwner,
		ErrorCode: errorCode, ErrorMessage: errorMessage, PayloadJSON: string(payload),
	}, usageJSON)
	if err != nil {
		service.logError(ctx, "finish agent task", "task_id", taskID, "error", err)

		return
	}

	if !changed {
		recovered, recoverErr := service.tasks.RecoverExpired(ctx, &database.TaskRecovery{
			Kind: database.TaskKindAgent, TargetState: database.TaskInterrupted,
			EventKind: taskInterruptedEvent, ErrorCode: "process_restart",
			ErrorMessage: "task interrupted after its worker lease expired",
			PayloadJSON:  `{"error_code":"process_restart"}`, ExpiresBefore: time.Now(),
		})
		if recoverErr != nil {
			service.logError(
				ctx, "recover expired agent task after unchanged finish", "task_id", taskID, "error", recoverErr,
			)

			return
		}

		for _, recoveredID := range recovered {
			service.publishLatest(ctx, recoveredID)
		}

		if !slices.Contains(recovered, taskID) {
			service.logWarn(ctx, "agent task finish was unchanged", "task_id", taskID)

			return
		}

		return
	}

	service.publishLatest(ctx, taskID)
}

func (service *Service) logError(ctx context.Context, message string, args ...any) {
	logger := service.logger
	if logger == nil {
		logger = slog.Default()
	}

	logger.ErrorContext(ctx, message, args...)
}

func (service *Service) logWarn(ctx context.Context, message string, args ...any) {
	logger := service.logger
	if logger == nil {
		logger = slog.Default()
	}

	logger.WarnContext(ctx, message, args...)
}

func (service *Service) publishLatest(ctx context.Context, taskID string) {
	event, found, err := service.tasks.LatestEvent(ctx, taskID)
	if err != nil || !found {
		return
	}

	service.publish(event)
}

func (service *Service) recoverInterrupted(ctx context.Context) error {
	recovered, err := service.tasks.RecoverExpired(ctx, &database.TaskRecovery{
		Kind: database.TaskKindAgent, TargetState: database.TaskInterrupted,
		EventKind: taskInterruptedEvent, ErrorCode: "process_restart",
		ErrorMessage: "task interrupted after its worker lease expired",
		PayloadJSON:  `{"error_code":"process_restart"}`, ExpiresBefore: time.Now(),
	})
	if err != nil {
		return oops.In("agenttask").Code("recover_tasks").Wrapf(err, "recover expired tasks")
	}

	for _, taskID := range recovered {
		service.publishLatest(ctx, taskID)
	}

	return nil
}

func (service *Service) enqueueRecovered(ctx context.Context) error {
	queued, err := service.tasks.ListByStates(ctx, database.TaskKindAgent, []database.TaskState{database.TaskQueued}, 0)
	if err != nil {
		return oops.In("agenttask").Code("recover_tasks").Wrapf(err, "list queued tasks")
	}

	for index := range queued {
		select {
		case service.queue <- queued[index].ID:
		case <-ctx.Done():
			return oops.In("agenttask").Code("recover_canceled").Wrapf(ctx.Err(), "enqueue recovered tasks")
		case <-service.done:
			return oops.In("agenttask").Code("service_stopped").Errorf("enqueue recovered tasks: service stopped")
		}
	}

	return nil
}

func newLeaseOwner() (string, error) {
	var identity [16]byte
	if _, err := rand.Read(identity[:]); err != nil {
		return "", oops.In("agenttask").Code("random_identity").Wrapf(err, "read random identity")
	}

	return hex.EncodeToString(identity[:]), nil
}

func persistedTimeout(policyJSON string) time.Duration {
	var snapshot struct {
		Limits struct {
			Timeout time.Duration `json:"timeout"`
		} `json:"limits"`
	}
	if json.Unmarshal([]byte(policyJSON), &snapshot) != nil {
		return 0
	}

	return snapshot.Limits.Timeout
}

func terminal(state database.TaskState) bool {
	return state == database.TaskSucceeded || state == database.TaskFailed ||
		state == database.TaskCanceled || state == database.TaskInterrupted
}
