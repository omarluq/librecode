// Package tooltask implements admission and execution of typed background tool tasks.
package tooltask

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/taskruntime"
	"github.com/omarluq/librecode/internal/tool"
)

const (
	outcomeResultKey          = "result"
	outcomeErrorKey           = "error"
	outcomeIsErrorKey         = "is_error"
	outcomeTruncatedKey       = "truncated"
	waitFallbackInterval      = time.Second
	fallbackDefaultTimeout    = 10 * time.Minute
	truncationDivisor         = 2
	completionSubscriberDepth = 32
)

// Eligible reports whether a built-in may be reconstructed as durable work.
// Eligibility is explicit rather than inferred from Definition.ReadOnly.
func Eligible(name tool.Name) bool {
	switch name {
	case tool.NameRead, tool.NameGrep, tool.NameFind, tool.NameLS, tool.NameAST,
		tool.NameBash, tool.NameEdit, tool.NameWrite:
		return true
	case tool.NameFetch:
		return false
	}

	return false
}

// CompletionHook applies assistant lifecycle semantics in the surviving worker.
type CompletionHook func(context.Context, *Completion) error

// Completion is the canonical target outcome presented to lifecycle hooks.
type Completion struct {
	Err            error
	Arguments      tool.Arguments
	TaskID         string
	InvocationID   string
	WrapperCallID  string
	ParentCallID   string
	OwnerSessionID string
	Target         string
	ArgumentsJSON  string
	Result         tool.Result
	SourceSequence int
}

// Invocation is immutable assistant metadata used for idempotent durable acceptance.
type Invocation struct {
	ID                string
	WrapperCallID     string
	ParentCallID      string
	OwnerSessionID    string
	CWD               string
	InitiatingEntryID string
	SourceSequence    int
}

// StartRequest requests asynchronous execution of an ordinary built-in tool.
type StartRequest struct {
	Admit      func(context.Context, *StartRequest) error
	Arguments  tool.Arguments
	Target     string
	Invocation Invocation
	Timeout    time.Duration
}

// Service admits tasks and reconstructs built-in executors in workers.
type Service struct {
	repository      *database.ToolTaskRepository
	runtime         *taskruntime.Service
	coordinator     *tool.Coordinator
	completionHook  CompletionHook
	admissions      map[string]*tool.PreparedCall
	completions     map[string]*Completion
	subscribers     map[uint64]chan Completion
	waiters         map[string]map[chan struct{}]struct{}
	starts          map[string]*startLock
	defaultTimeout  time.Duration
	maxTimeout      time.Duration
	admissionMu     sync.Mutex
	completionMu    sync.Mutex
	startMu         sync.Mutex
	waitMu          sync.Mutex
	nextSubscriber  uint64
	maxOutcomeBytes int
}

type startLock struct {
	mu   sync.Mutex
	refs int
}

// New constructs a background tool task service.
func New(
	repository *database.ToolTaskRepository,
	coordinator *tool.Coordinator,
	defaultTimeout time.Duration,
	maxTimeout time.Duration,
	maxOutcomeBytes int,
) *Service {
	if defaultTimeout <= 0 {
		defaultTimeout = fallbackDefaultTimeout
	}

	return &Service{
		repository: repository, runtime: nil, coordinator: coordinator, completionHook: nil,
		defaultTimeout: defaultTimeout, maxTimeout: maxTimeout, maxOutcomeBytes: maxOutcomeBytes,
		admissions: make(map[string]*tool.PreparedCall), admissionMu: sync.Mutex{},
		completions: make(map[string]*Completion), subscribers: make(map[uint64]chan Completion),
		completionMu: sync.Mutex{}, nextSubscriber: 0,
		startMu: sync.Mutex{}, starts: make(map[string]*startLock),
		waitMu: sync.Mutex{}, waiters: make(map[string]map[chan struct{}]struct{}),
	}
}

// AttachRuntime connects the scheduler used to wake and cancel task execution.
func (service *Service) AttachRuntime(runtime *taskruntime.Service) { service.runtime = runtime }

// SetCompletionHook installs the process lifecycle bridge before workers start.
func (service *Service) SetCompletionHook(hook CompletionHook) { service.completionHook = hook }

// SubscribeCompletions follows canonical outcomes after their terminal state is durably committed.
// Delivery is best effort and never blocks workers; Get remains the authoritative repair path.
func (service *Service) SubscribeCompletions() (events <-chan Completion, cancel func()) {
	service.completionMu.Lock()
	service.nextSubscriber++
	subscriberID := service.nextSubscriber
	channel := make(chan Completion, completionSubscriberDepth)
	service.subscribers[subscriberID] = channel
	service.completionMu.Unlock()

	var once sync.Once

	return channel, func() {
		once.Do(func() {
			service.completionMu.Lock()
			if registered, found := service.subscribers[subscriberID]; found {
				delete(service.subscribers, subscriberID)
				close(registered)
			}
			service.completionMu.Unlock()
		})
	}
}

// Start validates and durably admits a background tool invocation.
func (service *Service) Start(ctx context.Context, request *StartRequest) (*database.ToolTaskEntity, error) {
	if request == nil {
		return nil, oops.In("tooltask").Code("invalid_start_request").
			Errorf("start background tool task: request is required")
	}

	releaseStart := service.lockStart(request.Invocation.OwnerSessionID, request.Invocation.ID)
	defer releaseStart()

	name, existing, err := service.admitStartRequest(ctx, request)
	if err != nil || existing != nil {
		return existing, err
	}

	registry, err := tool.NewRegistryWithCoordinator(request.Invocation.CWD, []tool.Name{name}, service.coordinator)
	if err != nil {
		return nil, oops.In("tooltask").Code("create_registry").Wrapf(err, "create tool registry")
	}

	prepared, err := registry.Prepare(request.Target, request.Arguments)
	if err != nil {
		return nil, oops.In("tooltask").Code("prepare_tool").Wrapf(err, "prepare background tool")
	}

	prepared.Release()

	definitionJSON, err := admittedDefinitionJSON(registry, name)
	if err != nil {
		return nil, err
	}

	timeout := request.Timeout
	if timeout <= 0 {
		timeout = service.defaultTimeout
	}

	created, err := service.repository.Create(ctx, newToolTaskEntity(request, timeout, definitionJSON))
	if err != nil {
		return nil, oops.In("tooltask").Code("create_task").Wrapf(err, "create background tool task")
	}

	if service.runtime != nil {
		service.runtime.Notify()
	}

	return created, nil
}

func (service *Service) lockStart(owner, invocationID string) func() {
	key := owner + "\x00" + invocationID

	service.startMu.Lock()

	lock := service.starts[key]
	if lock == nil {
		lock = &startLock{mu: sync.Mutex{}, refs: 0}
		service.starts[key] = lock
	}

	lock.refs++
	service.startMu.Unlock()

	lock.mu.Lock()

	return func() {
		lock.mu.Unlock()

		service.startMu.Lock()

		lock.refs--
		if lock.refs == 0 {
			delete(service.starts, key)
		}
		service.startMu.Unlock()
	}
}

func (service *Service) admitStartRequest(
	ctx context.Context,
	request *StartRequest,
) (tool.Name, *database.ToolTaskEntity, error) {
	name := tool.Name(request.Target)
	if validationErr := validateStartRequest(request, name, service.maxTimeout); validationErr != nil {
		return name, nil, oops.In("tooltask").Code("invalid_start_request").Wrapf(
			validationErr, "validate background tool task",
		)
	}

	existing, found, err := service.repository.GetByInvocation(
		ctx, request.Invocation.OwnerSessionID, request.Invocation.ID,
	)
	if err != nil {
		return name, nil, oops.In("tooltask").Code("find_task").Wrapf(
			err, "find background tool task invocation",
		)
	}

	if found {
		return name, existing, nil
	}

	if request.Admit == nil {
		return name, nil, nil
	}

	if admissionErr := request.Admit(ctx, request); admissionErr != nil {
		return name, nil, oops.In("tooltask").Code("admit_task").Wrapf(
			admissionErr, "admit background tool task",
		)
	}

	name = tool.Name(request.Target)
	if validationErr := validateStartRequest(request, name, service.maxTimeout); validationErr != nil {
		return name, nil, oops.In("tooltask").Code("invalid_admitted_request").Wrapf(
			validationErr, "validate admitted background tool task",
		)
	}

	return name, nil, nil
}

func validateStartRequest(request *StartRequest, name tool.Name, maximumTimeout time.Duration) error {
	if !Eligible(name) {
		return fmt.Errorf("tool %q is not eligible for background execution", request.Target)
	}

	invocation := request.Invocation
	if strings.TrimSpace(invocation.ID) == "" || strings.TrimSpace(invocation.WrapperCallID) == "" ||
		strings.TrimSpace(invocation.OwnerSessionID) == "" {
		return errors.New("background task invocation identity is unavailable")
	}

	if !filepath.IsAbs(invocation.CWD) {
		return errors.New("background task working directory must be absolute")
	}

	if request.Timeout > maximumTimeout {
		return fmt.Errorf("timeout exceeds maximum %s", maximumTimeout)
	}

	return nil
}

func admittedDefinitionJSON(registry *tool.Registry, name tool.Name) (string, error) {
	for _, definition := range registry.Definitions() {
		if definition.Name != name {
			continue
		}

		encoded, err := json.Marshal(definition)
		if err != nil {
			return "", oops.In("tooltask").Code("encode_definition").Wrapf(err, "encode tool definition")
		}

		return string(encoded), nil
	}

	return "", errors.New("background tool definition is unavailable")
}

func newToolTaskEntity(request *StartRequest, timeout time.Duration, definitionJSON string) *database.ToolTaskEntity {
	invocation := request.Invocation

	return &database.ToolTaskEntity{
		OutcomeVersion: nil, OutcomeJSON: nil,
		Task: database.TaskEntity{
			CreatedAt: time.Time{}, StartedAt: nil, FinishedAt: nil, UpdatedAt: time.Time{}, LeaseExpiresAt: nil,
			ID: "", Kind: "", ParentTaskID: "", OwnerSessionID: "", ConcurrencyKey: "", LeaseOwner: "",
			State: "", Result: "", ErrorCode: "", ErrorMessage: "",
		},
		TargetName:    request.Target,
		ArgumentsJSON: request.Arguments.String(), CWD: invocation.CWD, OwnerSessionID: invocation.OwnerSessionID,
		InvocationID: invocation.ID, WrapperCallID: invocation.WrapperCallID, ParentCallID: invocation.ParentCallID,
		SourceSequence: invocation.SourceSequence, InitiatingEntryID: invocation.InitiatingEntryID,
		TimeoutSeconds: int((timeout + time.Second - 1) / time.Second), PolicyJSON: `{"eligible":true,"version":1}`,
		DefinitionJSON: definitionJSON,
	}
}

// Get returns an owner-scoped task snapshot.
func (service *Service) Get(ctx context.Context, owner, id string) (*database.ToolTaskEntity, bool, error) {
	entity, found, err := service.repository.GetOwned(ctx, owner, id)
	if err != nil {
		return nil, false, oops.In("tooltask").Code("get_task").Wrapf(err, "get background tool task")
	}

	return entity, found, nil
}

// Wait blocks until a task reaches a terminal state or ctx ends.
func (service *Service) Wait(ctx context.Context, owner, taskID string) (*database.ToolTaskEntity, error) {
	timer := time.NewTimer(waitFallbackInterval)
	stopTimer(timer)

	defer stopTimer(timer)

	for {
		wake := service.addWaiter(taskID)

		entity, found, err := service.Get(ctx, owner, taskID)
		if err != nil {
			service.removeWaiter(taskID, wake)

			return nil, err
		}

		if !found {
			service.removeWaiter(taskID, wake)

			return nil, errors.New("background task disappeared")
		}

		if isTerminal(entity.Task.State) {
			service.removeWaiter(taskID, wake)

			return entity, nil
		}

		timer.Reset(waitFallbackInterval)

		select {
		case <-ctx.Done():
			stopTimer(timer)
			service.removeWaiter(taskID, wake)

			return nil, oops.In("tooltask").Code("wait_canceled").Wrapf(ctx.Err(), "wait for background tool task")
		case <-wake:
			stopTimer(timer)
		case <-timer.C:
			service.removeWaiter(taskID, wake)
		}
	}
}

func stopTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}

func (service *Service) addWaiter(taskID string) chan struct{} {
	service.waitMu.Lock()
	defer service.waitMu.Unlock()

	wake := make(chan struct{})
	if service.waiters[taskID] == nil {
		service.waiters[taskID] = make(map[chan struct{}]struct{})
	}

	service.waiters[taskID][wake] = struct{}{}

	return wake
}

func (service *Service) notifyWaiters(taskID string) {
	service.waitMu.Lock()
	defer service.waitMu.Unlock()

	for wake := range service.waiters[taskID] {
		close(wake)
	}

	delete(service.waiters, taskID)
}

func (service *Service) removeWaiter(taskID string, wake chan struct{}) {
	service.waitMu.Lock()
	defer service.waitMu.Unlock()

	waiters := service.waiters[taskID]
	delete(waiters, wake)

	if len(waiters) == 0 {
		delete(service.waiters, taskID)
	}
}

func isTerminal(state database.TaskState) bool {
	switch state {
	case database.TaskSucceeded, database.TaskFailed, database.TaskCanceled, database.TaskInterrupted:
		return true
	case database.TaskQueued, database.TaskRunning, database.TaskCanceling:
		return false
	}

	return false
}

// List returns an owner's tasks, optionally filtered by state.
func (service *Service) List(
	ctx context.Context, owner string, states []database.TaskState, limit int,
) ([]database.ToolTaskEntity, error) {
	entities, err := service.repository.ListByOwner(ctx, owner, states, limit)
	if err != nil {
		return nil, oops.In("tooltask").Code("list_tasks").Wrapf(err, "list background tool tasks")
	}

	return entities, nil
}

// Cancel requests cancellation of an owner-scoped task.
func (service *Service) Cancel(ctx context.Context, owner, taskID string) (*database.ToolTaskEntity, bool, error) {
	task, found, err := service.repository.Cancel(ctx, owner, taskID)
	if err != nil {
		return nil, false, oops.In("tooltask").Code("cancel_task").Wrapf(err, "cancel background tool task")
	}

	if found {
		service.notifyWaiters(taskID)

		if service.runtime != nil {
			service.runtime.CancelActive(taskID)
		}
	}

	return task, found, nil
}

// Kind returns the durable task kind handled by this service.
func (service *Service) Kind() string { return database.TaskKindTool }

// TryAdmit reserves mutation resources before the authoritative task claim.
// Saturated resources leave the durable task queued rather than occupying a worker.
// A true result may accompany an error: admission succeeded, and the caller must
// continue dispatch so Run can durably settle the reported definition drift.
func (service *Service) TryAdmit(ctx context.Context, task *database.TaskEntity) (bool, error) {
	persisted, err := service.loadRunnableTask(ctx, task.ID)
	if err != nil {
		return false, err
	}

	call, err := service.preparePersistedCall(persisted)
	if err != nil {
		// Definition drift is settled by Run after claim; it must not strand the
		// task forever in the admission queue.
		return true, oops.In("tooltask").Code("admission_definition_drift").Wrapf(
			err, "defer definition drift to claimed execution",
		)
	}

	if !call.TryAdmit() {
		call.Release()

		return false, nil
	}

	service.admissionMu.Lock()
	previous := service.admissions[task.ID]
	service.admissions[task.ID] = &call
	service.admissionMu.Unlock()

	if previous != nil {
		previous.Release()
	}

	return true, nil
}

// ReleaseAdmission abandons any pre-claim reservation that was not consumed.
func (service *Service) ReleaseAdmission(taskID string) {
	service.admissionMu.Lock()
	call := service.admissions[taskID]
	delete(service.admissions, taskID)
	service.admissionMu.Unlock()

	if call != nil {
		call.Release()
	}
}

func (service *Service) admittedCall(taskID string) *tool.PreparedCall {
	service.admissionMu.Lock()
	defer service.admissionMu.Unlock()

	call := service.admissions[taskID]
	delete(service.admissions, taskID)

	return call
}

// Run reconstructs and executes a durably admitted tool invocation.
func (service *Service) Run(
	ctx context.Context, task *database.TaskEntity, _ taskruntime.EventSink,
) (taskruntime.Outcome, error) {
	persisted, err := service.loadRunnableTask(ctx, task.ID)
	if err != nil {
		return taskruntime.Outcome{Value: nil, Summary: "", ErrorCode: "", ErrorMessage: ""}, err
	}

	call := service.admittedCall(task.ID)
	if call == nil {
		prepared, prepareErr := service.preparePersistedCall(persisted)
		if prepareErr != nil {
			return taskruntime.Outcome{
				Value: nil, Summary: "", ErrorCode: "definition_drift", ErrorMessage: prepareErr.Error(),
			}, prepareErr
		}

		call = &prepared
	}
	defer call.Release()

	timeout := time.Duration(persisted.TimeoutSeconds) * time.Second
	if persisted.TimeoutSeconds <= 0 {
		timeout = service.defaultTimeout
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, runErr := call.Execute(runCtx)
	result, runErr = service.applyCompletionHook(ctx, task.ID, persisted, result, runErr)
	service.rememberCompletion(task.ID, persisted, result, runErr)
	value := outcomeValue(result, runErr)
	value, summary := boundOutcome(value, result.Text(), service.maxOutcomeBytes)

	return taskruntime.Outcome{
		Value: value, Summary: summary, ErrorCode: "", ErrorMessage: "",
	}, runErr
}

func (service *Service) loadRunnableTask(ctx context.Context, id string) (*database.ToolTaskEntity, error) {
	persisted, found, err := service.repository.Get(ctx, id)
	if err != nil {
		return nil, oops.In("tooltask").Code("load_task").Wrapf(err, "load background tool task")
	}

	if !found {
		return nil, errors.New("tool task not found")
	}

	if !Eligible(tool.Name(persisted.TargetName)) {
		return nil, errors.New("tool is no longer eligible")
	}

	return persisted, nil
}

func (service *Service) preparePersistedCall(persisted *database.ToolTaskEntity) (tool.PreparedCall, error) {
	arguments, err := tool.ArgumentsFromRaw([]byte(persisted.ArgumentsJSON))
	if err != nil {
		return tool.PreparedCall{}, oops.In("tooltask").Code("decode_arguments").Wrapf(err, "decode tool arguments")
	}

	name := tool.Name(persisted.TargetName)

	registry, err := tool.NewRegistryWithCoordinator(persisted.CWD, []tool.Name{name}, service.coordinator)
	if err != nil {
		return tool.PreparedCall{}, oops.In("tooltask").Code("create_registry").Wrapf(err, "create tool registry")
	}

	definitionJSON, err := admittedDefinitionJSON(registry, name)
	if err != nil {
		return tool.PreparedCall{}, err
	}

	if definitionJSON != persisted.DefinitionJSON {
		return tool.PreparedCall{}, errors.New("background tool definition changed after admission")
	}

	call, err := registry.Prepare(persisted.TargetName, arguments)
	if err != nil {
		return tool.PreparedCall{}, oops.In("tooltask").Code("prepare_tool").Wrapf(err, "prepare background tool")
	}

	return call, nil
}

func (service *Service) applyCompletionHook(
	ctx context.Context,
	taskID string,
	persisted *database.ToolTaskEntity,
	result tool.Result,
	runErr error,
) (tool.Result, error) {
	if service.completionHook == nil {
		return result, runErr
	}

	arguments, err := tool.ArgumentsFromRaw([]byte(persisted.ArgumentsJSON))
	if err != nil {
		service.recordCompletionHookFailure(ctx, taskID, oops.In("tooltask").Code("decode_arguments").Wrapf(
			err, "decode completion hook arguments",
		))

		return result, runErr
	}

	completion := newCompletion(taskID, persisted, arguments, result, runErr)

	hookErr := service.completionHook(ctx, completion)
	if hookErr == nil {
		return completion.Result, completion.Err
	}

	service.recordCompletionHookFailure(ctx, taskID, hookErr)

	return result, runErr
}

func newCompletion(
	taskID string,
	persisted *database.ToolTaskEntity,
	arguments tool.Arguments,
	result tool.Result,
	runErr error,
) *Completion {
	return &Completion{
		Err: runErr, Arguments: arguments, TaskID: taskID, InvocationID: persisted.InvocationID,
		WrapperCallID: persisted.WrapperCallID, ParentCallID: persisted.ParentCallID,
		OwnerSessionID: persisted.OwnerSessionID, Target: persisted.TargetName,
		ArgumentsJSON: persisted.ArgumentsJSON, Result: result, SourceSequence: persisted.SourceSequence,
	}
}

func (service *Service) rememberCompletion(
	taskID string, persisted *database.ToolTaskEntity, result tool.Result, runErr error,
) {
	arguments, err := tool.ArgumentsFromRaw([]byte(persisted.ArgumentsJSON))
	if err != nil {
		return
	}

	service.completionMu.Lock()
	service.completions[taskID] = newCompletion(taskID, persisted, arguments, result, runErr)
	service.completionMu.Unlock()
}

func (service *Service) publishCompletion(taskID string) {
	service.completionMu.Lock()
	completion := service.completions[taskID]
	delete(service.completions, taskID)

	if completion != nil {
		for _, subscriber := range service.subscribers {
			select {
			case subscriber <- *completion:
			default:
			}
		}
	}
	service.completionMu.Unlock()
}

func (service *Service) discardCompletion(taskID string) {
	service.completionMu.Lock()
	delete(service.completions, taskID)
	service.completionMu.Unlock()
}

func (service *Service) recordCompletionHookFailure(ctx context.Context, taskID string, hookErr error) {
	slog.Default().ErrorContext(ctx, "tool completion hook failed", "task_id", taskID, "error", hookErr)

	if service.repository == nil {
		return
	}

	payload, err := json.Marshal(map[string]string{"error": hookErr.Error()})
	if err != nil {
		slog.Default().ErrorContext(ctx, "encode tool completion hook failure", "task_id", taskID, "error", err)

		return
	}

	if _, err = service.repository.Tasks().AppendEvent(
		ctx, taskID, "tool_completion_hook_failed", string(payload),
	); err != nil {
		slog.Default().ErrorContext(
			ctx, "record tool completion hook failure", "task_id", taskID, "hook_error", hookErr, "error", err,
		)
	}
}

func outcomeValue(result tool.Result, runErr error) map[string]any {
	value := map[string]any{
		outcomeResultKey: result, outcomeErrorKey: "", outcomeIsErrorKey: false, outcomeTruncatedKey: false,
	}
	if runErr != nil {
		value[outcomeErrorKey] = runErr.Error()
		value[outcomeIsErrorKey] = true
	}

	return value
}

func boundOutcome(value map[string]any, summary string, maximum int) (bounded map[string]any, boundedSummary string) {
	if maximum <= 0 {
		return value, summary
	}

	encoded, err := json.Marshal(value)
	if err == nil && len(encoded) <= maximum {
		return value, summary
	}

	limit := min(len(summary), maximum/truncationDivisor)
	for {
		limit = runeBoundary(summary, limit)

		text := summary[:limit]
		bounded = map[string]any{
			outcomeResultKey: tool.TextResult(text, map[string]any{outcomeTruncatedKey: true}),
			outcomeErrorKey:  value[outcomeErrorKey], outcomeIsErrorKey: value[outcomeIsErrorKey],
			outcomeTruncatedKey: true,
		}

		encoded, marshalErr := json.Marshal(bounded)
		if marshalErr == nil && len(encoded) <= maximum {
			return bounded, text
		}

		if limit == 0 {
			return map[string]any{
				outcomeErrorKey:   "outcome exceeded persistence limit",
				outcomeIsErrorKey: true, outcomeTruncatedKey: true,
			}, ""
		}

		limit /= truncationDivisor
	}
}

func runeBoundary(value string, limit int) int {
	for limit > 0 && limit < len(value) && !utf8.RuneStart(value[limit]) {
		limit--
	}

	return limit
}

// RecoverExpired interrupts tasks whose worker leases have expired.
func (service *Service) RecoverExpired(ctx context.Context, expiresBefore time.Time) error {
	if err := service.repository.RecoverExpired(ctx, expiresBefore); err != nil {
		return oops.In("tooltask").Code("recover_tasks").Wrapf(err, "recover background tool tasks")
	}

	service.admissionMu.Lock()

	admissionIDs := make([]string, 0, len(service.admissions))
	for taskID := range service.admissions {
		admissionIDs = append(admissionIDs, taskID)
	}
	service.admissionMu.Unlock()

	for _, taskID := range admissionIDs {
		persisted, found, err := service.repository.Get(ctx, taskID)
		if err != nil {
			return oops.In("tooltask").Code("load_recovered_task").Wrapf(err, "load recovered background tool task")
		}

		if !found || isTerminal(persisted.Task.State) {
			service.ReleaseAdmission(taskID)
		}
	}

	return nil
}

// Settle atomically persists the canonical outcome and terminal transition.
func (service *Service) Settle(
	ctx context.Context, finish *database.TaskFinish, outcome taskruntime.Outcome,
) (bool, error) {
	encoded, err := json.Marshal(outcome.Value)
	if err != nil {
		return false, oops.In("tooltask").Code("encode_outcome").Wrapf(err, "encode tool task outcome")
	}

	changed, err := service.repository.Finish(ctx, finish, string(encoded))
	if err != nil {
		return false, oops.In("tooltask").Code("settle_task").Wrapf(err, "settle background tool task")
	}

	if changed {
		service.notifyWaiters(finish.TaskID)
		service.publishCompletion(finish.TaskID)
	} else {
		service.discardCompletion(finish.TaskID)
	}

	return changed, nil
}
