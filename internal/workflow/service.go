package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/database"
)

var errRunClaimConflict = errors.New("workflow run is not claimable")

const (
	defaultSourceVersion  = "v1"
	workflowStartedEvent  = "workflow_started"
	workflowResumedEvent  = "workflow_resumed"
	workflowEventKind     = "workflow_event"
	workflowSucceeded     = "workflow_succeeded"
	workflowFailed        = "workflow_failed"
	workflowCanceled      = "workflow_canceled"
	workflowInterrupted   = "workflow_interrupted"
	workflowLeaseDuration = 30 * time.Second
	workflowLeaseInterval = 5 * time.Second
	workflowAwaitInterval = 100 * time.Millisecond
)

// ServiceRequest describes one durable one-shot workflow execution.
type ServiceRequest struct {
	Name           string
	Source         string
	SourceVersion  string
	ArgumentsJSON  string
	OwnerSessionID string
}

type eventPayload struct {
	Task            TaskResult `json:"task"`
	Kind            EventKind  `json:"kind"`
	TaskID          string     `json:"task_id"`
	NodeKey         string     `json:"node_key,omitempty"`
	InvocationIndex int        `json:"invocation_index,omitempty"`
}

type runResultPayload struct {
	Value           any          `json:"value"`
	Stdout          string       `json:"stdout"`
	Stderr          string       `json:"stderr"`
	LaunchedTaskIDs []string     `json:"launched_task_ids"`
	TaskResults     []TaskResult `json:"task_results"`
}

// Service persists workflow lifecycle and delegates source evaluation to a Runner.
type Service struct {
	runs       *database.WorkflowRepository
	runner     *Runner
	active     map[string]context.CancelFunc
	leaseOwner string
	mu         sync.Mutex
}

// NewService creates the durable workflow execution boundary.
func NewService(runs *database.WorkflowRepository, runner *Runner) (*Service, error) {
	if runs == nil || runner == nil {
		return nil, oops.In("workflow").Code("invalid_service_dependencies").
			Errorf("workflow repository and runner are required")
	}

	leaseOwner, err := uuid.NewV7()
	if err != nil {
		return nil, oops.In("workflow").Code("create_lease_owner").Wrapf(err, "create workflow lease owner")
	}

	return &Service{
		runs: runs, runner: runner, active: make(map[string]context.CancelFunc),
		leaseOwner: leaseOwner.String(), mu: sync.Mutex{},
	}, nil
}

// Submit durably queues a workflow run without executing it.
func (service *Service) Submit(
	ctx context.Context,
	request *ServiceRequest,
) (*database.WorkflowRunEntity, error) {
	return service.createRun(ctx, request)
}

// Run creates and executes one durable workflow run.
func (service *Service) Run(
	ctx context.Context,
	request *ServiceRequest,
) (*database.WorkflowRunEntity, *RunResult, error) {
	run, persisted, arguments, err := service.prepareRun(ctx, request)
	if err != nil {
		return run, nil, err
	}

	return service.executeExisting(ctx, run, persisted, arguments, request.Name, nil, false)
}

// ExecuteQueued claims and executes a previously submitted workflow run.
// A false return means another worker claimed the run first.
func (service *Service) ExecuteQueued(ctx context.Context, runID string) (bool, error) {
	run, err := service.loadVerifiedRun(ctx, runID)
	if err != nil {
		return false, err
	}

	arguments, err := decodeArguments(run.ArgumentsJSON)
	if err != nil {
		return false, err
	}

	completed, _, err := service.executeExisting(ctx, run, run, arguments, run.Name, nil, false)
	if err != nil && errors.Is(err, errRunClaimConflict) {
		return false, nil
	}

	// Execution failures are durable workflow outcomes, not dispatcher failures.
	// Once the terminal state has been persisted, callers can inspect the run
	// without the background worker also writing the source error to the TUI.
	if err != nil && completed != nil && terminal(completed.Task.State) {
		return true, nil
	}

	return true, err
}

// Await waits for a workflow run to reach a terminal state.
func (service *Service) Await(ctx context.Context, runID string) (*database.WorkflowRunEntity, error) {
	ticker := time.NewTicker(workflowAwaitInterval)
	defer ticker.Stop()

	for {
		run, found, err := service.Get(ctx, runID)
		if err != nil {
			return nil, err
		}

		if !found {
			return nil, oops.In("workflow").Code("missing_run").Errorf("workflow run was not found")
		}

		if terminal(run.Task.State) {
			return run, nil
		}

		select {
		case <-ctx.Done():
			return nil, oops.In("workflow").Code("await_canceled").Wrapf(ctx.Err(), "await workflow run")
		case <-ticker.C:
		}
	}
}

// Resume replays an interrupted workflow while reusing its persisted child invocations.
func (service *Service) Resume(
	ctx context.Context,
	runID string,
) (*database.WorkflowRunEntity, *RunResult, error) {
	run, err := service.loadVerifiedRun(ctx, runID)
	if err != nil {
		return nil, nil, err
	}

	arguments, err := decodeArguments(run.ArgumentsJSON)
	if err != nil {
		return run, nil, err
	}

	links, err := service.runs.ListAgentTasks(ctx, runID)
	if err != nil {
		return run, nil, oops.In("workflow").Code("load_agent_links").Wrapf(err, "load workflow agent links")
	}

	return service.executeExisting(ctx, run, run, arguments, run.Name, links, true)
}

func (service *Service) executeExisting(
	ctx context.Context,
	run *database.WorkflowRunEntity,
	persisted *database.WorkflowRunEntity,
	arguments map[string]any,
	name string,
	links []database.WorkflowAgentTaskEntity,
	resume bool,
) (*database.WorkflowRunEntity, *RunResult, error) {
	claim := &database.TaskClaim{TaskID: run.Task.ID, LeaseOwner: service.leaseOwner,
		EventKind: workflowStartedEvent, LeaseExpiresAt: time.Now().UTC().Add(workflowLeaseDuration)}
	claimRun := service.runs.Tasks().ClaimQueued

	if resume {
		claim.EventKind = workflowResumedEvent
		claimRun = service.runs.Tasks().ClaimInterrupted
	}

	changed, err := claimRun(ctx, claim)
	if err != nil {
		return run, nil, oops.In("workflow").Code("start_run").Wrapf(err, "start workflow run")
	}

	if !changed {
		return run, nil, oops.In("workflow").Code("start_run_conflict").
			Wrapf(errRunClaimConflict, "workflow run is not resumable")
	}

	runCtx, cancelRun := context.WithCancel(ctx)
	heartbeatCtx, stopHeartbeat := context.WithCancel(runCtx)

	service.track(run.Task.ID, cancelRun)

	defer func() {
		stopHeartbeat()
		cancelRun()
		service.untrack(run.Task.ID)
	}()

	heartbeatErr := service.monitorLease(heartbeatCtx, run.Task.ID, cancelRun)
	result, runErr := service.runner.Run(runCtx, &RunRequest{
		RunID: persisted.Task.ID, OnEvent: service.eventSink(persisted.Task.ID), Name: name,
		Source: persisted.Source, OwnerSessionID: persisted.Task.OwnerSessionID, Arguments: arguments,
		PersistedLinks: links,
	})

	stopHeartbeat()

	if heartbeatRunErr := <-heartbeatErr; heartbeatRunErr != nil {
		runErr = errors.Join(runErr, heartbeatRunErr)
	}

	if finishErr := service.finish(runCtx, run.Task.ID, &result, runErr); finishErr != nil {
		return run, &result, errors.Join(runErr, finishErr)
	}

	completed, found, err := service.runs.Get(context.WithoutCancel(ctx), run.Task.ID)
	if err != nil {
		return run, &result, oops.In("workflow").Code("reload_run").Wrapf(err, "reload workflow run")
	}

	if !found {
		return run, &result, oops.In("workflow").Code("missing_run").Errorf("completed workflow run was not found")
	}

	return completed, &result, runErr
}

func (service *Service) prepareRun(
	ctx context.Context,
	request *ServiceRequest,
) (run, persisted *database.WorkflowRunEntity, arguments map[string]any, err error) {
	if request == nil {
		return nil, nil, nil, oops.In("workflow").Code("invalid_service_request").Errorf("request is required")
	}

	run, err = service.createRun(ctx, request)
	if err != nil {
		return nil, nil, nil, err
	}

	persisted, err = service.loadVerifiedRun(ctx, run.Task.ID)
	if err != nil {
		return run, nil, nil, err
	}

	arguments, err = decodeArguments(persisted.ArgumentsJSON)
	if err != nil {
		return run, nil, nil, err
	}

	return run, persisted, arguments, nil
}

// RecoverInterrupted marks abandoned in-process runs interrupted after restart.
// Workflow source is not replayed because interpreter memory is intentionally not persisted.
func (service *Service) RecoverInterrupted(ctx context.Context) ([]string, error) {
	runIDs, err := service.runs.Tasks().RecoverExpired(ctx, &database.TaskRecovery{
		ExpiresBefore: time.Now().UTC(), Kind: database.TaskKindWorkflow,
		EventKind: workflowInterrupted, ErrorCode: "process_restart",
		ErrorMessage: "workflow interrupted by process restart", PayloadJSON: "{}",
		TargetState: database.TaskInterrupted,
	})
	if err != nil {
		return nil, oops.In("workflow").Code("recover_runs").Wrapf(err, "recover interrupted workflow runs")
	}

	return runIDs, nil
}

// Cancel cancels an active run owned by the supplied session.
func (service *Service) Cancel(ctx context.Context, ownerSessionID, runID string) (bool, error) {
	run, found, err := service.runs.Get(ctx, runID)
	if err != nil {
		return false, oops.In("workflow").Code("get_run_for_cancel").Wrapf(err, "get workflow run")
	}

	if !found {
		return false, nil
	}

	if run.Task.OwnerSessionID != ownerSessionID {
		return false, nil
	}

	changed, err := service.runs.Tasks().Transition(
		ctx, runID, []database.TaskState{database.TaskQueued}, database.TaskCanceled, workflowCanceled,
	)
	if err != nil {
		return false, oops.In("workflow").Code("cancel_queued_run").Wrapf(err, "cancel queued workflow run")
	}

	if !changed {
		changed, err = service.runs.Tasks().Transition(
			ctx, runID, []database.TaskState{database.TaskRunning}, database.TaskCanceling, workflowCanceled,
		)
		if err != nil {
			return false, oops.In("workflow").Code("cancel_running_run").Wrapf(err, "cancel running workflow run")
		}
	}

	service.mu.Lock()
	cancel := service.active[runID]
	service.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	return changed, nil
}

// Get returns one workflow run.
func (service *Service) Get(ctx context.Context, runID string) (*database.WorkflowRunEntity, bool, error) {
	run, found, err := service.runs.Get(ctx, runID)
	if err != nil {
		return nil, false, oops.In("workflow").Code("get_run").Wrapf(err, "get workflow run")
	}

	return run, found, nil
}

// List returns workflow runs owned by a session.
func (service *Service) List(
	ctx context.Context,
	ownerSessionID string,
	limit int,
) ([]database.WorkflowRunEntity, error) {
	runs, err := service.runs.ListByOwner(ctx, ownerSessionID, limit)
	if err != nil {
		return nil, oops.In("workflow").Code("list_runs").Wrapf(err, "list workflow runs")
	}

	return runs, nil
}

// Events returns durable workflow events in replay order.
func (service *Service) Events(
	ctx context.Context,
	runID string,
	after int64,
	limit int,
) ([]database.TaskEventEntity, error) {
	events, err := service.runs.Tasks().ListEvents(ctx, runID, after, limit)
	if err != nil {
		return nil, oops.In("workflow").Code("list_events").Wrapf(err, "list workflow events")
	}

	return events, nil
}

// AgentTask returns one agent task linked from a workflow.
func (service *Service) AgentTask(ctx context.Context, taskID string) (*database.AgentTaskEntity, bool, error) {
	task, found, err := service.runs.AgentTasks().Get(ctx, taskID)
	if err != nil {
		return nil, false, oops.In("workflow").Code("get_agent_task").Wrapf(err, "get workflow agent task")
	}

	return task, found, nil
}

// AgentTasks returns workflow child links in launch order.
func (service *Service) AgentTasks(
	ctx context.Context,
	runID string,
) ([]database.WorkflowAgentTaskEntity, error) {
	tasks, err := service.runs.ListAgentTasks(ctx, runID)
	if err != nil {
		return nil, oops.In("workflow").Code("list_agent_tasks").Wrapf(err, "list workflow agent tasks")
	}

	return tasks, nil
}

func (service *Service) createRun(
	ctx context.Context,
	request *ServiceRequest,
) (*database.WorkflowRunEntity, error) {
	if request == nil {
		return nil, oops.In("workflow").Code("invalid_service_request").Errorf("request is required")
	}

	sourceVersion := request.SourceVersion
	if sourceVersion == "" {
		sourceVersion = defaultSourceVersion
	}

	argumentsJSON := request.ArgumentsJSON
	if argumentsJSON == "" {
		argumentsJSON = "{}"
	}

	hash := sha256.Sum256([]byte(request.Source))

	run, err := service.runs.Create(ctx, &database.WorkflowRunEntity{
		Task: database.TaskEntity{
			CreatedAt: time.Time{}, StartedAt: nil, FinishedAt: nil, UpdatedAt: time.Time{},
			LeaseExpiresAt: nil, ID: "", Kind: "", ParentTaskID: "", OwnerSessionID: request.OwnerSessionID,
			ConcurrencyKey: request.OwnerSessionID, LeaseOwner: "", State: "", Result: "",
			ErrorCode: "", ErrorMessage: "",
		},
		Name: request.Name, Source: request.Source, SourceHash: hex.EncodeToString(hash[:]),
		SourceVersion: sourceVersion, ArgumentsJSON: argumentsJSON,
	})
	if err != nil {
		return nil, oops.In("workflow").Code("create_run").Wrapf(err, "create workflow run")
	}

	return run, nil
}

func decodeArguments(argumentsJSON string) (map[string]any, error) {
	arguments := map[string]any{}
	if err := json.Unmarshal([]byte(argumentsJSON), &arguments); err != nil {
		return nil, oops.In("workflow").Code("decode_arguments").Wrapf(err, "decode workflow arguments")
	}

	return arguments, nil
}

func (service *Service) loadVerifiedRun(
	ctx context.Context,
	runID string,
) (*database.WorkflowRunEntity, error) {
	run, found, err := service.runs.Get(ctx, runID)
	if err != nil {
		return nil, oops.In("workflow").Code("load_persisted_run").Wrapf(err, "load persisted workflow run")
	}

	if !found {
		return nil, oops.In("workflow").Code("missing_persisted_run").Errorf("persisted workflow run was not found")
	}

	hash := sha256.Sum256([]byte(run.Source))
	if hex.EncodeToString(hash[:]) != run.SourceHash {
		return nil, oops.In("workflow").Code("source_hash_mismatch").Errorf("persisted workflow source hash differs")
	}

	return run, nil
}

func (service *Service) eventSink(runID string) EventSink {
	return func(ctx context.Context, event Event) error {
		payload, err := json.Marshal(eventPayload(event))
		if err != nil {
			return oops.In("workflow").Code("encode_event").Wrapf(err, "encode workflow event")
		}

		if _, err := service.runs.Tasks().AppendEvent(ctx, runID, workflowEventKind, string(payload)); err != nil {
			return oops.In("workflow").Code("persist_event").Wrapf(err, "persist workflow event")
		}

		return nil
	}
}

func (service *Service) finish(ctx context.Context, runID string, result *RunResult, runErr error) error {
	encoded, encodeErr := json.Marshal(runResultPayload{
		Value: result.Value, Stdout: result.Stdout, Stderr: result.Stderr,
		LaunchedTaskIDs: result.LaunchedTaskIDs, TaskResults: result.TaskResults,
	})
	if encodeErr != nil {
		runErr = errors.Join(runErr, encodeErr)
	}

	state, eventKind, errorCode, errorMessage := workflowOutcome(ctx, runErr)

	changed, err := service.runs.Tasks().Finish(context.WithoutCancel(ctx), &database.TaskFinish{
		TaskID: runID, EventKind: eventKind, Result: string(encoded), ErrorCode: errorCode,
		ErrorMessage: errorMessage, PayloadJSON: "{}", LeaseOwner: service.leaseOwner, TargetState: state,
		From: []database.TaskState{database.TaskRunning, database.TaskCanceling},
	})
	if err != nil {
		return oops.In("workflow").Code("finish_run").Wrapf(err, "finish workflow run")
	}

	if !changed {
		return oops.In("workflow").Code("finish_run_conflict").Errorf("workflow run was not running")
	}

	if encodeErr != nil {
		return oops.In("workflow").Code("encode_result").Wrapf(encodeErr, "encode workflow result")
	}

	return nil
}

func workflowOutcome(
	ctx context.Context,
	runErr error,
) (state database.TaskState, eventKind, errorCode, errorMessage string) {
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(runErr, context.Canceled) {
		return database.TaskCanceled, workflowCanceled, "canceled", errorString(runErr)
	}

	if runErr != nil {
		return database.TaskFailed, workflowFailed, "workflow_failed", runErr.Error()
	}

	return database.TaskSucceeded, workflowSucceeded, "", ""
}

func errorString(err error) string {
	if err == nil {
		return ""
	}

	return err.Error()
}

func (service *Service) monitorLease(
	ctx context.Context,
	runID string,
	cancel context.CancelFunc,
) <-chan error {
	result := make(chan error, 1)

	go func() {
		defer close(result)

		ticker := time.NewTicker(workflowLeaseInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				result <- nil

				return
			case <-ticker.C:
				stopped, err := service.monitorLeaseTick(ctx, runID)
				if stopped {
					result <- nil

					return
				}

				if err != nil {
					cancel()

					result <- err

					return
				}
			}
		}
	}()

	return result
}

func (service *Service) monitorLeaseTick(ctx context.Context, runID string) (stopped bool, err error) {
	if contextDone(ctx) {
		return true, nil
	}

	err = service.renewLease(ctx, runID)

	return contextDone(ctx), err
}

func contextDone(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

func (service *Service) renewLease(ctx context.Context, runID string) error {
	renewed, err := service.runs.Tasks().RenewLease(
		ctx, runID, service.leaseOwner, time.Now().UTC().Add(workflowLeaseDuration),
	)
	if err != nil {
		return oops.In("workflow").Code("renew_lease").Wrapf(err, "renew workflow lease")
	}

	if !renewed {
		return oops.In("workflow").Code("lease_lost").Errorf("workflow lease was lost")
	}

	task, found, err := service.runs.Tasks().Get(ctx, runID)
	if err != nil {
		return oops.In("workflow").Code("read_lease_state").Wrapf(err, "read workflow state")
	}

	if !found {
		return oops.In("workflow").Code("missing_lease_task").Errorf("workflow lease task was not found")
	}

	if task.State == database.TaskCanceling {
		return context.Canceled
	}

	return nil
}

func (service *Service) track(runID string, cancel context.CancelFunc) {
	service.mu.Lock()
	defer service.mu.Unlock()

	service.active[runID] = cancel
}

func (service *Service) untrack(runID string) {
	service.mu.Lock()
	defer service.mu.Unlock()

	delete(service.active, runID)
}
