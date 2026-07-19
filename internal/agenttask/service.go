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
	"sync"
	"time"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
)

const (
	defaultConcurrency         = 4
	defaultSessionConcurrency  = 2
	defaultTimeout             = 30 * time.Minute
	defaultQueueCapacity       = 256
	awaitPollInterval          = time.Second
	dispatchRetryInterval      = 10 * time.Millisecond
	finalizeTimeout            = 10 * time.Second
	leaseDuration              = 30 * time.Second
	leaseHeartbeatInterval     = 10 * time.Second
	leaseRenewalRetryInterval  = 250 * time.Millisecond
	leaseRenewalAttemptTimeout = 2 * time.Second
	leaseRenewalAttempts       = 3
	eventBuffer                = 64
	enqueueTaskOperation       = "enqueue task"
	enqueueCanceledCode        = "enqueue_canceled"
	enqueueCanceledMessage     = "task submission was canceled before queue admission"
	serviceStoppedCode         = "service_stopped"
	serviceStoppedMessage      = "task service stopped before queue admission"
)

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
	runner                     Runner
	getTaskFn                  func(context.Context, string) (*database.TaskEntity, bool, error)
	renewLeaseFn               func(context.Context, string, string, time.Time) (bool, error)
	active                     map[string]context.CancelFunc
	subscribers                map[string]map[uint64]chan database.TaskEventEntity
	agentTasks                 *database.AgentTaskRepository
	workflows                  *database.WorkflowRepository
	queue                      chan string
	cancel                     context.CancelFunc
	done                       <-chan struct{}
	sessionSlots               map[string]chan struct{}
	tasks                      *database.TaskRepository
	logger                     *slog.Logger
	leaseOwner                 string
	wg                         sync.WaitGroup
	nextSubscriber             uint64
	timeout                    time.Duration
	sessionConcurrency         int
	leaseDuration              time.Duration
	leaseHeartbeatInterval     time.Duration
	leaseRenewalRetryInterval  time.Duration
	leaseRenewalAttemptTimeout time.Duration
	leaseRenewalAttempts       int
	mu                         sync.Mutex
}

func invalidOptions(options *Options) bool {
	return options == nil || options.Tasks == nil || options.AgentTasks == nil || options.Runner == nil
}

// New creates and starts a task service.
func New(ctx context.Context, options *Options) (*Service, error) {
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

	serviceCtx, cancel := context.WithCancel(ctx)

	leaseOwner, err := newLeaseOwner()
	if err != nil {
		cancel()

		return nil, oops.In("agenttask").Code("worker_identity").Wrapf(err, "create worker identity")
	}

	service := &Service{
		runner: options.Runner, done: serviceCtx.Done(), tasks: options.Tasks,
		agentTasks: options.AgentTasks, workflows: options.Workflows, queue: make(chan string, queueCapacity),
		cancel: cancel, active: make(map[string]context.CancelFunc),
		sessionSlots:   make(map[string]chan struct{}),
		subscribers:    make(map[string]map[uint64]chan database.TaskEventEntity),
		nextSubscriber: 0, wg: sync.WaitGroup{}, timeout: timeout,
		sessionConcurrency: sessionConcurrency, logger: logger, leaseOwner: leaseOwner,
		getTaskFn: options.Tasks.Get, renewLeaseFn: options.Tasks.RenewLease, leaseDuration: leaseDuration,
		leaseHeartbeatInterval:     leaseHeartbeatInterval,
		leaseRenewalRetryInterval:  leaseRenewalRetryInterval,
		leaseRenewalAttemptTimeout: leaseRenewalAttemptTimeout,
		leaseRenewalAttempts:       leaseRenewalAttempts, mu: sync.Mutex{},
	}
	if err := service.recoverInterrupted(ctx); err != nil {
		cancel()

		return nil, err
	}

	for range concurrency {
		service.wg.Add(1)
		go service.worker(serviceCtx)
	}

	if err := service.enqueueRecovered(ctx, serviceCtx); err != nil {
		cancel()
		service.wg.Wait()

		return nil, err
	}

	return service, nil
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
	}}
}

// SubscribeAgentTask exposes task completion notifications through the assistant boundary.
func (service *Service) SubscribeAgentTask(
	taskID string,
) (events <-chan database.TaskEventEntity, cancel func()) {
	subscription := service.Subscribe(taskID)

	return subscription.Events, subscription.Cancel
}

// Cancel requests cancellation without allowing terminal states to change.
func (service *Service) Cancel(
	ctx context.Context,
	ownerSessionID string,
	taskID string,
) (*database.TaskEntity, bool, error) {
	owned, err := service.ownsTask(ctx, ownerSessionID, taskID)
	if err != nil || !owned {
		return nil, false, err
	}

	changed, err := service.tasks.Transition(
		ctx, taskID, []database.TaskState{database.TaskQueued}, database.TaskCanceled, "task_canceled",
	)
	if err != nil {
		return nil, false, oops.In("agenttask").Code("cancel_task").Wrapf(err, "cancel queued task")
	}

	if changed {
		service.publishLatest(ctx, taskID)
	}

	if !changed {
		changed, err = service.tasks.Transition(
			ctx, taskID, []database.TaskState{database.TaskRunning}, database.TaskCanceling, "task_canceling",
		)
		if err != nil {
			return nil, false, oops.In("agenttask").Code("cancel_task").Wrapf(err, "cancel running task")
		}

		if changed {
			service.publishLatest(ctx, taskID)
			service.mu.Lock()
			cancel := service.active[taskID]
			service.mu.Unlock()

			if cancel != nil {
				cancel()
			}
		}
	}

	task, found, err := service.tasks.Get(ctx, taskID)
	if err != nil {
		return nil, false, oops.In("agenttask").Code("get_task").Wrapf(err, "get canceled task")
	}

	return task, found, nil
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

	ticker := time.NewTicker(awaitPollInterval)
	defer ticker.Stop()

	events := subscription.Events

	for {
		task, found, err := service.agentTasks.Get(ctx, taskID)
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
	service.cancel()
	service.closeSubscriptions()

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
		select {
		case <-ctx.Done():
			return
		case taskID := <-service.queue:
			service.run(ctx, taskID)
		}
	}
}

func (service *Service) run(ctx context.Context, taskID string) {
	task, found, err := service.agentTasks.Get(ctx, taskID)
	if err != nil {
		service.logError(ctx, "load queued agent task", "task_id", taskID, "error", err)
		service.requeue(ctx, taskID)

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

	result, runErr := service.runner.Run(runCtx, task, service.eventSink(taskID))
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

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !service.renewLeaseWithRetry(ctx, taskID) {
				cancel()

				return
			}
		}
	}
}

func (service *Service) renewLeaseWithRetry(ctx context.Context, taskID string) bool {
	for attempt := 1; attempt <= service.leaseRenewalAttempts; attempt++ {
		renewed, err := service.attemptLeaseRenewal(ctx, taskID)
		if err == nil {
			return service.handleLeaseRenewal(ctx, taskID, attempt, renewed)
		}

		if ctx.Err() != nil {
			return false
		}

		if attempt == service.leaseRenewalAttempts {
			service.logError(ctx, "renew agent task lease after retries", "task_id", taskID,
				"lease_owner", service.leaseOwner, "attempts", attempt, "error", err)

			return false
		}

		service.logWarn(ctx, "retry agent task lease renewal", "task_id", taskID,
			"lease_owner", service.leaseOwner, "attempt", attempt,
			"max_attempts", service.leaseRenewalAttempts,
			"retry_after", service.leaseRenewalRetryInterval, "error", err)

		if !waitForLeaseRenewalRetry(ctx, service.leaseRenewalRetryInterval) {
			return false
		}
	}

	return false
}

func (service *Service) attemptLeaseRenewal(ctx context.Context, taskID string) (bool, error) {
	attemptCtx, cancel := context.WithTimeout(ctx, service.leaseRenewalAttemptTimeout)
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
		message := "task canceled"
		if runErr != nil {
			message = runErr.Error()
		}

		service.finish(ctx, taskID, database.TaskCanceled, "task_canceled", result, "canceled", message)

		return
	}

	if runErr == nil {
		service.finish(ctx, taskID, database.TaskSucceeded, "task_succeeded", result, "", "")

		return
	}

	if ctx.Err() != nil {
		service.finish(
			ctx, taskID, database.TaskInterrupted, "task_interrupted", result,
			"service_stopped", "task interrupted by service shutdown",
		)

		return
	}

	service.finish(ctx, taskID, database.TaskFailed, "task_failed", result, "run_failed", runErr.Error())
}

func (service *Service) eventSink(taskID string) EventSink {
	return func(ctx context.Context, kind string, payload any) error {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return oops.In("agenttask").Code("marshal_event").Wrapf(err, "marshal task event")
		}

		event, err := service.tasks.AppendEvent(ctx, taskID, kind, string(encoded))
		if err != nil {
			return oops.In("agenttask").Code("append_event").Wrapf(err, "append task event")
		}

		service.publish(event)

		return nil
	}
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
	case "task_succeeded", "task_failed", "task_canceled", "task_interrupted":
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
	_, err := service.tasks.RecoverExpired(ctx, &database.TaskRecovery{
		Kind: database.TaskKindAgent, TargetState: database.TaskInterrupted,
		EventKind: "task_interrupted", ErrorCode: "process_restart",
		ErrorMessage: "task interrupted after its worker lease expired",
		PayloadJSON:  `{"error_code":"process_restart"}`, ExpiresBefore: time.Now(),
	})
	if err != nil {
		return oops.In("agenttask").Code("recover_tasks").Wrapf(err, "recover expired tasks")
	}

	return nil
}

func (service *Service) enqueueRecovered(ctx, serviceCtx context.Context) error {
	queued, err := service.tasks.ListByStates(ctx, database.TaskKindAgent, []database.TaskState{database.TaskQueued}, 0)
	if err != nil {
		return oops.In("agenttask").Code("recover_tasks").Wrapf(err, "list queued tasks")
	}

	for index := range queued {
		select {
		case service.queue <- queued[index].ID:
		case <-ctx.Done():
			return oops.In("agenttask").Code("recover_canceled").Wrapf(ctx.Err(), "enqueue recovered tasks")
		case <-serviceCtx.Done():
			return oops.In("agenttask").Code("service_stopped").Wrapf(serviceCtx.Err(), "enqueue recovered tasks")
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
