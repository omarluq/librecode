// Package agenttask owns durable asynchronous agent execution.
package agenttask

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	awaitPollInterval         = 50 * time.Millisecond
	dispatchRetryInterval     = 10 * time.Millisecond
	finalizeTimeout           = 10 * time.Second
	eventBuffer               = 64
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
	Tasks              *database.TaskRepository
	AgentTasks         *database.AgentTaskRepository
	Runner             Runner
	Concurrency        int
	SessionConcurrency int
	QueueCapacity      int
	Timeout            time.Duration
}

// Service schedules and owns durable agent tasks.
type Service struct {
	runner             Runner
	ctx                context.Context
	tasks              *database.TaskRepository
	agentTasks         *database.AgentTaskRepository
	queue              chan string
	cancel             context.CancelFunc
	active             map[string]context.CancelFunc
	sessionSlots       map[string]chan struct{}
	subscribers        map[string]map[uint64]chan database.TaskEventEntity
	nextSubscriber     uint64
	wg                 sync.WaitGroup
	timeout            time.Duration
	sessionConcurrency int
	mu                 sync.Mutex
}

// New creates and starts a task service.
func New(ctx context.Context, options Options) (*Service, error) {
	if options.Tasks == nil || options.AgentTasks == nil || options.Runner == nil {
		return nil, errors.New("agenttask: tasks, agent tasks, and runner are required")
	}

	if ctx == nil {
		return nil, errors.New("agenttask: process context is required")
	}

	concurrency, sessionConcurrency, queueCapacity, timeout := optionDefaults(options)

	serviceCtx, cancel := context.WithCancel(ctx)

	service := &Service{
		runner: options.Runner, ctx: serviceCtx, tasks: options.Tasks,
		agentTasks: options.AgentTasks, queue: make(chan string, queueCapacity),
		cancel: cancel, active: make(map[string]context.CancelFunc),
		sessionSlots:   make(map[string]chan struct{}),
		subscribers:    make(map[string]map[uint64]chan database.TaskEventEntity),
		nextSubscriber: 0, wg: sync.WaitGroup{}, timeout: timeout,
		sessionConcurrency: sessionConcurrency, mu: sync.Mutex{},
	}
	if err := service.recoverInterrupted(ctx); err != nil {
		cancel()

		return nil, err
	}

	for range concurrency {
		service.wg.Add(1)
		go service.worker()
	}

	if err := service.enqueueRecovered(ctx); err != nil {
		cancel()
		service.wg.Wait()

		return nil, err
	}

	return service, nil
}

func optionDefaults(options Options) (
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
	return service.Submit(ctx, &SubmitRequest{
		ParentTaskID:   "",
		OwnerSessionID: request.OwnerSessionID,
		ChildSessionID: request.ChildSessionID,
		ConcurrencyKey: request.OwnerSessionID,
		AgentName:      request.AgentName,
		Prompt:         request.Prompt,
		Model:          request.Model,
		Provider:       request.Provider,
		PolicyJSON:     request.PolicyJSON,
		Depth:          request.Depth,
	})
}

// Submit durably accepts a task before making it available to workers.
func (service *Service) Submit(ctx context.Context, request *SubmitRequest) (*database.AgentTaskEntity, error) {
	created, err := service.agentTasks.Create(ctx, &database.AgentTaskEntity{
		Task: database.TaskEntity{
			ID: "", Kind: "", ParentTaskID: request.ParentTaskID,
			OwnerSessionID: request.OwnerSessionID, ConcurrencyKey: request.ConcurrencyKey,
			State: "", Result: "", ErrorCode: "", ErrorMessage: "",
			CreatedAt: time.Time{}, StartedAt: nil, FinishedAt: nil, UpdatedAt: time.Time{},
		},
		ChildSessionID: request.ChildSessionID, AgentName: request.AgentName,
		Prompt: request.Prompt, Model: request.Model, Provider: request.Provider,
		PolicyJSON: request.PolicyJSON, UsageJSON: "{}", Depth: request.Depth,
	})
	if err != nil {
		return nil, oops.In("agenttask").Code("create_task").Wrapf(err, "create agent task")
	}

	select {
	case service.queue <- created.Task.ID:
		return created, nil
	case <-service.ctx.Done():
		return created, oops.In("agenttask").Code("service_stopped").Wrapf(service.ctx.Err(), "enqueue task")
	case <-ctx.Done():
		service.rejectQueuedTask(
			created.Task.ID, "enqueue_canceled", "task submission was canceled before queue admission",
		)

		return created, oops.In("agenttask").Code("enqueue_canceled").Wrapf(ctx.Err(), "enqueue task")
	default:
		service.rejectQueuedTask(created.Task.ID, "queue_full", "agent task queue is full")

		return created, oops.In("agenttask").Code("queue_full").Errorf("agent task queue is full")
	}
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
func (service *Service) List(ctx context.Context, ownerSessionID string, limit int) ([]database.TaskEntity, error) {
	tasks, err := service.tasks.ListByOwner(ctx, database.TaskKindAgent, ownerSessionID, limit)
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
	ticker := time.NewTicker(awaitPollInterval)
	defer ticker.Stop()

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

func (service *Service) worker() {
	defer service.wg.Done()

	for {
		select {
		case <-service.ctx.Done():
			return
		case taskID := <-service.queue:
			service.run(taskID)
		}
	}
}

func (service *Service) run(taskID string) {
	task, found, err := service.agentTasks.Get(service.ctx, taskID)
	if err != nil || !found {
		return
	}

	release, acquired := service.acquireSessionSlot(task.Task.ConcurrencyKey)
	if !acquired {
		service.requeue(taskID)

		return
	}
	defer release()

	changed, err := service.tasks.Transition(
		service.ctx, taskID, []database.TaskState{database.TaskQueued}, database.TaskRunning, "task_started",
	)
	if err != nil || !changed {
		return
	}

	service.publishLatest(service.ctx, taskID)

	task, found, err = service.agentTasks.Get(service.ctx, taskID)
	if err != nil || !found {
		service.finish(
			taskID, database.TaskFailed, "task_failed",
			Result{Text: "", UsageJSON: ""}, "load_task", "load agent task",
		)

		return
	}

	result, runErr := service.execute(taskID, task)
	service.finalizeRun(taskID, result, runErr)
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
		return func() {}, false
	}
}

func (service *Service) requeue(taskID string) {
	timer := time.NewTimer(dispatchRetryInterval)
	defer timer.Stop()

	select {
	case <-service.ctx.Done():
		return
	case <-timer.C:
	}

	select {
	case service.queue <- taskID:
	case <-service.ctx.Done():
	}
}

func (service *Service) rejectQueuedTask(taskID, errorCode, errorMessage string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(service.ctx), finalizeTimeout)
	defer cancel()

	changed, err := service.tasks.Finish(
		ctx, taskID, []database.TaskState{database.TaskQueued}, database.TaskFailed,
		"task_failed", "", errorCode, errorMessage, `{"error_code":"`+errorCode+`"}`,
	)
	if err == nil && changed {
		service.publishLatest(ctx, taskID)
	}
}

func (service *Service) execute(taskID string, task *database.AgentTaskEntity) (Result, error) {
	timeout := service.timeout
	if taskTimeout := persistedTimeout(task.PolicyJSON); taskTimeout > 0 {
		timeout = taskTimeout
	}

	runCtx, cancel := context.WithTimeout(service.ctx, timeout)
	service.mu.Lock()
	service.active[taskID] = cancel
	service.mu.Unlock()

	// Cancellation can race with registration in active. Re-read durable state
	// after registration so a canceling task never starts model or tool work.
	current, found, err := service.tasks.Get(context.WithoutCancel(service.ctx), taskID)
	if err == nil && found && current.State == database.TaskCanceling {
		cancel()
	}

	result, runErr := service.runner.Run(runCtx, task, service.eventSink(taskID))

	cancel()

	service.mu.Lock()
	delete(service.active, taskID)
	service.mu.Unlock()

	if runErr != nil {
		return result, oops.In("agenttask").Code("execute_task").Wrapf(runErr, "execute agent task")
	}

	return result, nil
}

func (service *Service) finalizeRun(taskID string, result Result, runErr error) {
	current, found, err := service.tasks.Get(context.WithoutCancel(service.ctx), taskID)
	if err == nil && found && current.State == database.TaskCanceling {
		message := "task canceled"
		if runErr != nil {
			message = runErr.Error()
		}

		service.finish(taskID, database.TaskCanceled, "task_canceled", result, "canceled", message)

		return
	}

	if runErr == nil {
		service.finish(taskID, database.TaskSucceeded, "task_succeeded", result, "", "")

		return
	}

	if service.ctx.Err() != nil {
		service.finish(
			taskID, database.TaskInterrupted, "task_interrupted", result,
			"service_stopped", "task interrupted by service shutdown",
		)

		return
	}

	service.finish(taskID, database.TaskFailed, "task_failed", result, "run_failed", runErr.Error())
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

	for _, subscriber := range service.subscribers[event.TaskID] {
		select {
		case subscriber <- *event:
		default:
		}
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
	taskID string,
	state database.TaskState,
	kind string,
	result Result,
	errorCode string,
	errorMessage string,
) {
	payload, err := json.Marshal(map[string]string{"error_code": errorCode})
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(service.ctx), finalizeTimeout)
	defer cancel()

	usageJSON := result.UsageJSON
	if usageJSON == "" {
		usageJSON = "{}"
	}

	changed, err := service.agentTasks.Finish(
		ctx, taskID,
		[]database.TaskState{database.TaskRunning, database.TaskCanceling},
		state, kind, result.Text, usageJSON, errorCode, errorMessage, string(payload),
	)
	if err != nil || !changed {
		return
	}

	service.publishLatest(ctx, taskID)
}

func (service *Service) publishLatest(ctx context.Context, taskID string) {
	event, found, err := service.tasks.LatestEvent(ctx, taskID)
	if err != nil || !found {
		return
	}

	service.publish(event)
}

func (service *Service) recoverInterrupted(ctx context.Context) error {
	interrupted, err := service.tasks.ListByStates(
		ctx, database.TaskKindAgent, []database.TaskState{database.TaskRunning, database.TaskCanceling}, 0,
	)
	if err != nil {
		return oops.In("agenttask").Code("recover_tasks").Wrapf(err, "list interrupted tasks")
	}

	for index := range interrupted {
		if _, finishErr := service.tasks.Finish(
			ctx, interrupted[index].ID,
			[]database.TaskState{database.TaskRunning, database.TaskCanceling},
			database.TaskInterrupted, "task_interrupted", "", "process_restart",
			"task interrupted by process restart", `{"error_code":"process_restart"}`,
		); finishErr != nil {
			return oops.In("agenttask").Code("recover_task").Wrapf(finishErr, "interrupt task")
		}
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
		case <-service.ctx.Done():
			return oops.In("agenttask").Code("service_stopped").Wrapf(service.ctx.Err(), "enqueue recovered tasks")
		}
	}

	return nil
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
