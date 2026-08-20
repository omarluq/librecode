// Package taskruntime provides bounded, durable scheduling for typed background tasks.
package taskruntime

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
	"unicode/utf8"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/database"
)

// EventSink appends a durable task observation.
type EventSink func(context.Context, string, any) error

// Outcome is a handler's canonical structured result and searchable summary.
type Outcome struct {
	Value        any    `json:"value,omitempty"`
	Summary      string `json:"summary,omitempty"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// Handler reconstructs and executes one durable task kind.
type Handler interface {
	Kind() string
	Run(context.Context, *database.TaskEntity, EventSink) (Outcome, error)
}

// Settler lets a typed repository commit its canonical outcome in the same
// transaction as the terminal lifecycle transition.
type Settler interface {
	Settle(context.Context, *database.TaskFinish, Outcome) (bool, error)
}

// Recoverer atomically settles expired owned work for a typed task kind.
type Recoverer interface {
	RecoverExpired(context.Context, time.Time) error
}

// Admitter reserves execution resources while a task is still queued.
type Admitter interface {
	TryAdmit(context.Context, *database.TaskEntity) (bool, error)
	ReleaseAdmission(string)
}

// Options configures the generic task-table polling scheduler.
type Options struct {
	Tasks             *database.TaskRepository
	Logger            *slog.Logger
	Workers           int
	PollInterval      time.Duration
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
	RecoveryInterval  time.Duration
	DefaultTimeout    time.Duration
	MaxPayloadBytes   int
}

const (
	defaultWorkers         = 4
	defaultPollInterval    = 250 * time.Millisecond
	defaultLeaseDuration   = 30 * time.Second
	defaultTimeout         = 30 * time.Minute
	defaultMaxPayloadBytes = 256 * 1024
	leaseIdentityBytes     = 16
	heartbeatLeaseFraction = 3
	errorCodePayloadRatio  = 8
	messagePayloadRatio    = 2
	unknownKindCode        = "unknown_kind"
)

// Service owns all scheduler and execution goroutines.
type Service struct {
	pollDone     chan struct{}
	handlers     map[string]Handler
	cancel       context.CancelFunc
	wake         chan struct{}
	sem          chan struct{}
	active       map[string]context.CancelFunc
	stopped      chan struct{}
	leaseOwner   string
	handlerOrder []string
	options      Options
	wg           sync.WaitGroup
	nextHandler  int
	dbMu         sync.RWMutex
	stopOnce     sync.Once
	mu           sync.Mutex
	started      bool
	abandoned    bool
}

// New constructs a task runtime with defaults for omitted scheduling options.
func New(options Options, handlers ...Handler) (*Service, error) {
	options, err := normalizeOptions(options)
	if err != nil {
		return nil, err
	}

	identity := make([]byte, leaseIdentityBytes)
	if _, err = rand.Read(identity); err != nil {
		return nil, oops.In("taskruntime").Code("generate_identity").Wrapf(err, "generate lease owner")
	}

	service := &Service{
		pollDone: make(chan struct{}), handlers: map[string]Handler{}, cancel: nil,
		wake: make(chan struct{}, 1), sem: make(chan struct{}, options.Workers),
		active: map[string]context.CancelFunc{}, stopped: make(chan struct{}),
		leaseOwner: hex.EncodeToString(identity), handlerOrder: nil, nextHandler: 0, options: options,
		wg: sync.WaitGroup{}, stopOnce: sync.Once{},
		started: false, abandoned: false,
		dbMu: sync.RWMutex{}, mu: sync.Mutex{},
	}

	if err := service.registerHandlers(handlers); err != nil {
		return nil, err
	}

	return service, nil
}

func normalizeOptions(options Options) (Options, error) {
	if options.Tasks == nil {
		return Options{}, oops.In("taskruntime").Code("nil_tasks").Errorf("task repository is required")
	}

	if options.Workers <= 0 {
		options.Workers = defaultWorkers
	}

	if options.PollInterval <= 0 {
		options.PollInterval = defaultPollInterval
	}

	if options.LeaseDuration <= 0 {
		options.LeaseDuration = defaultLeaseDuration
	}

	if options.HeartbeatInterval <= 0 {
		options.HeartbeatInterval = options.LeaseDuration / heartbeatLeaseFraction
	}

	if options.RecoveryInterval <= 0 {
		options.RecoveryInterval = options.LeaseDuration
	}

	if options.DefaultTimeout <= 0 {
		options.DefaultTimeout = defaultTimeout
	}

	if options.MaxPayloadBytes <= 0 {
		options.MaxPayloadBytes = defaultMaxPayloadBytes
	}

	if options.HeartbeatInterval >= options.LeaseDuration {
		return Options{}, oops.In("taskruntime").Code("invalid_heartbeat").Errorf(
			"heartbeat must be shorter than lease duration",
		)
	}

	return options, nil
}

func (service *Service) registerHandlers(handlers []Handler) error {
	for _, handler := range handlers {
		if handler == nil || handler.Kind() == "" {
			return oops.In("taskruntime").Code("invalid_handler").Errorf("task handler and kind are required")
		}

		if _, ok := service.handlers[handler.Kind()]; ok {
			return oops.In("taskruntime").Code("duplicate_handler").Errorf(
				"duplicate handler %q", handler.Kind(),
			)
		}

		service.handlers[handler.Kind()] = handler
		service.handlerOrder = append(service.handlerOrder, handler.Kind())
	}

	return nil
}

// Start begins polling and recovering durable tasks.
func (service *Service) Start(parent context.Context) error {
	service.mu.Lock()

	if service.started {
		service.mu.Unlock()

		return nil
	}

	if parent == nil {
		service.mu.Unlock()

		return errors.New("taskruntime: context is required")
	}

	runtimeCtx, cancel := context.WithCancel(parent)
	service.cancel = cancel
	service.started = true
	service.mu.Unlock()

	// Reconcile already-expired leases before queued work is dispatched so the
	// restored lifecycle snapshot is authoritative from startup.
	service.recover(runtimeCtx)
	go service.poll(runtimeCtx)

	service.Notify()

	return nil
}

// Notify wakes the task-table poller after durable acceptance.
func (service *Service) Notify() {
	select {
	case service.wake <- struct{}{}:
	default:
	}
}

func (service *Service) poll(ctx context.Context) {
	defer close(service.pollDone)

	ticker := time.NewTicker(service.options.PollInterval)
	recovery := time.NewTicker(service.options.RecoveryInterval)

	defer ticker.Stop()
	defer recovery.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			service.dispatch(ctx)
		case <-service.wake:
			service.dispatch(ctx)
		case <-recovery.C:
			service.recover(ctx)
		}
	}
}

func (service *Service) dispatch(ctx context.Context) {
	service.rejectUnknownKinds(ctx)

	handlerCount := len(service.handlerOrder)

	startHandler := service.nextHandler
	if handlerCount > 0 {
		service.nextHandler = (service.nextHandler + 1) % handlerCount
	}

	for offset := range handlerCount {
		if len(service.sem) == cap(service.sem) {
			return
		}

		handlerIndex := (startHandler + offset) % handlerCount
		kind := service.handlerOrder[handlerIndex]

		tasks, err := service.options.Tasks.ListByStates(
			ctx, kind, []database.TaskState{database.TaskQueued}, 0,
		)
		if err != nil {
			if ctx.Err() == nil {
				service.log("list queued tasks", err)
			}

			continue
		}

		if service.dispatchTasks(ctx, kind, tasks) {
			return
		}
	}
}

func (service *Service) rejectUnknownKinds(ctx context.Context) {
	// externallyHandledKinds are dispatched by specialized runtimes rather than
	// handlers registered with this generic service.
	externallyHandledKinds := []string{database.TaskKindAgent, database.TaskKindWorkflow}
	knownKinds := make([]string, 0, len(externallyHandledKinds)+len(service.handlerOrder))
	knownKinds = append(knownKinds, externallyHandledKinds...)
	knownKinds = append(knownKinds, service.handlerOrder...)

	tasks, err := service.options.Tasks.ListQueuedExcluding(ctx, knownKinds, cap(service.sem))
	if err != nil {
		if ctx.Err() == nil {
			service.log("list unknown task kinds", err)
		}

		return
	}

	for index := range tasks {
		finish := &database.TaskFinish{
			TaskID: tasks[index].ID, From: []database.TaskState{database.TaskQueued},
			TargetState: database.TaskFailed, EventKind: "task_failed", Result: "",
			ErrorCode: unknownKindCode, ErrorMessage: "no task handler registered",
			PayloadJSON: `{"error_code":"unknown_kind"}`, LeaseOwner: "",
		}
		if _, finishErr := service.options.Tasks.Finish(ctx, finish); finishErr != nil {
			service.log("reject unknown task kind", finishErr)
		}
	}
}

func (service *Service) dispatchTasks(ctx context.Context, kind string, tasks []database.TaskEntity) bool {
	handler := service.handlers[kind]
	admitter, usesAdmission := handler.(Admitter)

	for taskIndex := range tasks {
		select {
		case service.sem <- struct{}{}:
		default:
			return true
		}

		if usesAdmission {
			admitted, err := admitter.TryAdmit(ctx, &tasks[taskIndex])
			if err != nil {
				service.log("admit queued task", err)
			}

			if !admitted {
				<-service.sem

				continue
			}
		}

		service.wg.Add(1)
		go service.run(ctx, tasks[taskIndex].ID, kind)
	}

	return false
}

func (service *Service) run(ctx context.Context, taskID, kind string) {
	defer service.wg.Done()
	defer func() { <-service.sem; service.Notify() }()

	handler := service.handlers[kind]
	if admitter, ok := handler.(Admitter); ok {
		defer admitter.ReleaseAdmission(taskID)
	}

	claim := &database.TaskClaim{
		TaskID: taskID, LeaseOwner: service.leaseOwner,
		LeaseExpiresAt: time.Now().Add(service.options.LeaseDuration), EventKind: "task_started",
	}

	changed, err := service.options.Tasks.ClaimQueued(ctx, claim)
	if err != nil || !changed {
		if err != nil {
			service.log("claim task", err)
		}

		return
	}

	service.runClaimed(ctx, taskID, handler)
}

func (service *Service) runClaimed(ctx context.Context, taskID string, handler Handler) {
	task, found, err := service.options.Tasks.Get(ctx, taskID)
	if err != nil || !found {
		service.finish(ctx, taskID, database.TaskFailed, Outcome{
			Value: nil, Summary: "", ErrorCode: "load_task", ErrorMessage: "load claimed task",
		})

		return
	}

	if handler == nil {
		service.finish(ctx, taskID, database.TaskFailed, Outcome{
			Value: nil, Summary: "", ErrorCode: unknownKindCode, ErrorMessage: "no task handler registered",
		})

		return
	}

	runCtx, cancel := context.WithTimeout(ctx, service.options.DefaultTimeout)
	service.mu.Lock()
	service.active[taskID] = cancel
	service.mu.Unlock()

	heartbeatDone := make(chan struct{})
	go service.heartbeat(runCtx, cancel, taskID, heartbeatDone)

	current, found, stateErr := service.options.Tasks.Get(context.WithoutCancel(ctx), taskID)

	var (
		outcome   Outcome
		runErr    error
		canceling bool
	)

	switch {
	case stateErr != nil:
		runErr = stateErr
	case !found:
		runErr = errors.New("task disappeared")
	case current.State == database.TaskCanceling:
		canceling = true
		runErr = context.Canceled
	default:
		outcome, runErr = service.safeRun(runCtx, handler, task, service.eventSink(taskID))
	}

	cancel()
	<-heartbeatDone
	service.mu.Lock()
	delete(service.active, taskID)
	service.mu.Unlock()

	canceling = service.confirmCanceling(ctx, taskID, canceling)
	service.settle(ctx, taskID, handler, outcome, runErr, canceling)
}

func (service *Service) confirmCanceling(ctx context.Context, taskID string, alreadyCanceling bool) bool {
	if alreadyCanceling {
		return true
	}

	current, found, err := service.options.Tasks.Get(context.WithoutCancel(ctx), taskID)
	if err != nil {
		service.log("load task state before settlement", err)

		return false
	}

	return found && current.State == database.TaskCanceling
}

func (service *Service) safeRun(
	ctx context.Context, handler Handler, task *database.TaskEntity, sink EventSink,
) (outcome Outcome, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = oops.In("taskruntime").Code("handler_panic").Errorf("task handler panicked: %v", recovered)
		}
	}()

	outcome, err = handler.Run(ctx, task, sink)
	if err != nil {
		return outcome, oops.In("taskruntime").Code("handler_failed").Wrapf(err, "run task handler")
	}

	return outcome, nil
}
func (service *Service) heartbeat(ctx context.Context, cancel context.CancelFunc, taskID string, done chan<- struct{}) {
	defer close(done)

	ticker := time.NewTicker(service.options.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			renewed, err := service.options.Tasks.RenewLease(
				ctx, taskID, service.leaseOwner, time.Now().Add(service.options.LeaseDuration),
			)
			if err != nil {
				service.logLeaseRenewal(taskID, err)
				cancel()

				return
			}

			if !renewed {
				service.logLeaseRenewal(taskID, oops.In("taskruntime").Code("lease_lost").
					Errorf("task lease renewal did not update a row"))
				cancel()

				return
			}
		}
	}
}
func (service *Service) eventSink(taskID string) EventSink {
	return func(ctx context.Context, kind string, value any) error {
		service.dbMu.RLock()
		defer service.dbMu.RUnlock()

		service.mu.Lock()
		abandoned := service.abandoned
		service.mu.Unlock()

		if abandoned {
			return context.Canceled
		}

		encoded, err := json.Marshal(value)
		if err != nil {
			return oops.In("taskruntime").Code("encode_event").Wrapf(err, "encode task event")
		}

		if len(encoded) > service.options.MaxPayloadBytes {
			return fmt.Errorf("task event payload exceeds %d bytes", service.options.MaxPayloadBytes)
		}

		_, appended, err := service.options.Tasks.AppendRunningEvent(
			ctx, taskID, service.leaseOwner, kind, string(encoded),
		)
		if err != nil {
			return oops.In("taskruntime").Code("append_event").Wrapf(err, "append task event")
		}

		if !appended {
			return oops.In("taskruntime").Code("lease_lost").Errorf("task lease is no longer owned")
		}

		return nil
	}
}
func (service *Service) settle(
	ctx context.Context,
	taskID string,
	handler Handler,
	outcome Outcome,
	runErr error,
	canceling bool,
) {
	state := database.TaskSucceeded

	if canceling {
		state = database.TaskCanceled
	} else if runErr != nil {
		state = database.TaskFailed
		if ctx.Err() != nil {
			state = database.TaskInterrupted
			outcome.ErrorCode = "service_stopped"
		}

		if outcome.ErrorCode == "" {
			outcome.ErrorCode = "run_failed"
		}

		if outcome.ErrorMessage == "" {
			outcome.ErrorMessage = runErr.Error()
		}
	}

	outcome.ErrorCode = boundText(outcome.ErrorCode, service.options.MaxPayloadBytes/errorCodePayloadRatio)
	outcome.ErrorMessage = boundText(outcome.ErrorMessage, service.options.MaxPayloadBytes/messagePayloadRatio)
	outcome.Summary = boundText(outcome.Summary, service.options.MaxPayloadBytes/messagePayloadRatio)
	service.finishWithHandler(ctx, taskID, handler, state, outcome)
}

func boundText(value string, maximum int) string {
	if maximum <= 0 || len(value) <= maximum {
		return value
	}

	for maximum > 0 && !utf8.RuneStart(value[maximum]) {
		maximum--
	}

	return value[:maximum]
}
func (service *Service) finish(
	ctx context.Context, taskID string, state database.TaskState, outcome Outcome,
) {
	service.finishWithHandler(ctx, taskID, nil, state, outcome)
}

func (service *Service) finishWithHandler(
	ctx context.Context, taskID string, handler Handler, state database.TaskState, outcome Outcome,
) {
	service.mu.Lock()
	abandoned := service.abandoned
	service.mu.Unlock()

	if abandoned {
		return
	}

	kinds := map[database.TaskState]string{
		database.TaskSucceeded: "task_succeeded", database.TaskFailed: "task_failed",
		database.TaskCanceled: "task_canceled", database.TaskInterrupted: "task_interrupted",
	}
	kind := kinds[state]

	payload, marshalErr := json.Marshal(outcome)
	if marshalErr != nil {
		service.log("encode task outcome", marshalErr)

		return
	}

	finish := &database.TaskFinish{
		TaskID: taskID, From: []database.TaskState{database.TaskRunning, database.TaskCanceling},
		TargetState: state, EventKind: kind, Result: outcome.Summary,
		ErrorCode: outcome.ErrorCode, ErrorMessage: outcome.ErrorMessage,
		PayloadJSON: string(payload), LeaseOwner: service.leaseOwner,
	}

	service.dbMu.RLock()
	defer service.dbMu.RUnlock()

	service.mu.Lock()
	abandoned = service.abandoned
	service.mu.Unlock()

	if abandoned {
		return
	}

	settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), service.options.LeaseDuration)
	defer cancel()

	var (
		changed bool
		err     error
	)
	if settler, ok := handler.(Settler); ok {
		changed, err = settler.Settle(settleCtx, finish, outcome)
	} else {
		changed, err = service.options.Tasks.Finish(settleCtx, finish)
	}

	if err != nil {
		service.log("finish task", err)

		return
	}

	if !changed {
		service.log("finish task", oops.In("taskruntime").Code("settlement_unchanged").
			Errorf("task settlement did not transition task %s", taskID))
	}
}
func (service *Service) recover(ctx context.Context) {
	for _, kind := range service.handlerOrder {
		handler := service.handlers[kind]

		expiresBefore := time.Now()
		if recoverer, ok := handler.(Recoverer); ok {
			if err := recoverer.RecoverExpired(ctx, expiresBefore); err != nil {
				service.log("recover typed tasks", err)
			}

			continue
		}

		recovery := &database.TaskRecovery{
			Kind: kind, TargetState: database.TaskInterrupted, EventKind: "task_interrupted",
			ErrorCode: "lease_expired", ErrorMessage: "task worker lease expired",
			PayloadJSON: `{"error_code":"lease_expired"}`, ExpiresBefore: expiresBefore,
		}

		_, err := service.options.Tasks.RecoverExpired(ctx, recovery)
		if err != nil {
			service.log("recover tasks", err)
		}
	}
}

// CancelActive interrupts a locally running task after durable cancellation is requested.
func (service *Service) CancelActive(taskID string) {
	service.mu.Lock()
	cancel := service.active[taskID]
	service.mu.Unlock()

	if cancel != nil {
		cancel()
	}
}

// Shutdown stops polling and waits for active tasks until the context expires.
func (service *Service) Shutdown(ctx context.Context) error {
	if ctx == nil {
		return errors.New("taskruntime: shutdown context is required")
	}

	service.mu.Lock()
	if !service.started {
		service.mu.Unlock()

		return nil
	}

	service.cancel()

	for _, cancel := range service.active {
		cancel()
	}
	service.mu.Unlock()

	// The poller is the sole source of worker WaitGroup additions. Observe its
	// exit within the caller's deadline before beginning the worker wait.
	select {
	case <-service.pollDone:
	case <-ctx.Done():
		service.abandon()

		return oops.In("taskruntime").Code("shutdown_timeout").Wrapf(ctx.Err(), "wait for task poller")
	}

	service.stopOnce.Do(func() {
		go func() {
			service.wg.Wait()
			close(service.stopped)
		}()
	})

	select {
	case <-service.stopped:
		return nil
	case <-ctx.Done():
		// Fence future events and settlement from late non-cooperative handlers.
		// Repository work already in flight may finish after this deadline.
		service.abandon()

		return oops.In("taskruntime").Code("shutdown_timeout").Wrapf(ctx.Err(), "wait for task workers")
	}
}

func (service *Service) abandon() {
	// Do not wait for in-flight repository I/O after the shutdown deadline.
	// Event and settlement paths check this flag before database operations.
	service.mu.Lock()
	service.abandoned = true
	service.mu.Unlock()
}

func (service *Service) logLeaseRenewal(taskID string, err error) {
	logger := service.options.Logger
	if logger == nil {
		logger = slog.Default()
	}

	logger.Error("renew task lease", "task_id", taskID, "lease_owner", service.leaseOwner, "error", err)
}

func (service *Service) log(message string, err error) {
	if err == nil {
		return
	}

	logger := service.options.Logger
	if logger == nil {
		logger = slog.Default()
	}

	logger.Error(message, "error", err)
}
