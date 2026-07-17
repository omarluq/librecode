// Package workflow executes MVM source that coordinates durable agent tasks.
package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/executeworker"
)

const (
	cancelTimeout         = 5 * time.Second
	pipelineResultKind    = "pipeline_result"
	taskNotOwnedBySession = "task is not owned by this workflow session"
)

// EventKind identifies workflow progress.
type EventKind string

const (
	// EventTaskLaunched reports a newly submitted task.
	EventTaskLaunched EventKind = "task_launched"
	// EventTaskCompleted reports a completed wait.
	EventTaskCompleted EventKind = "task_completed"
)

// Event is an observable workflow progress update.
type Event struct {
	Task            TaskResult
	Kind            EventKind
	TaskID          string
	NodeKey         string
	InvocationIndex int
}

// EventSink receives workflow progress synchronously.
type EventSink func(context.Context, Event) error

// AgentOptions configures one durable agent launch. The controller supplies any
// product-specific defaults not provided by the workflow.
type AgentOptions struct {
	NodeKey        string `json:"node_key"`
	AgentName      string `json:"agent_name"`
	Model          string `json:"model"`
	Provider       string `json:"provider"`
	ConcurrencyKey string `json:"concurrency_key"`
	Depth          int    `json:"depth"`
}

// AgentRequest describes a durable agent operation requested by a workflow.
type AgentRequest struct {
	ParentTaskID    string
	OwnerSessionID  string
	NodeKey         string
	Prompt          string
	Options         AgentOptions
	InvocationIndex int
}

// Controller is the narrow durable-agent boundary used by workflow runs.
type Controller interface {
	Submit(context.Context, *AgentRequest) (*database.AgentTaskEntity, error)
	Get(context.Context, string) (*database.AgentTaskEntity, bool, error)
	Await(context.Context, string) (*database.AgentTaskEntity, error)
	Cancel(context.Context, string, string) (*database.TaskEntity, bool, error)
}

// RunRequest describes one isolated workflow evaluation.
type RunRequest struct {
	RunID          string
	Name           string
	Source         string
	OwnerSessionID string
	OnEvent        EventSink
	Arguments      map[string]any
	PersistedLinks []database.WorkflowAgentTaskEntity
}

// RunResult contains script output and the tasks launched by this run.
type RunResult struct {
	Value           any
	Stdout          string
	Stderr          string
	LaunchedTaskIDs []string
	TaskResults     []TaskResult
}

// TaskResult is the stable workflow-facing view of a durable task.
type TaskResult struct {
	ID           string `json:"id"`
	State        string `json:"state"`
	Result       string `json:"result"`
	ErrorCode    string `json:"error_code"`
	ErrorMessage string `json:"error_message"`
}

// Runner evaluates workflow source against a durable agent controller.
type Runner struct {
	controller Controller
	executable string
}

// NewRunner creates a workflow runner.
func NewRunner(controller Controller) (*Runner, error) {
	if controller == nil {
		return nil, oops.In("workflow").Code("invalid_controller").Errorf("controller is required")
	}

	return &Runner{controller: controller, executable: ""}, nil
}

// Run evaluates source and waits for any Agent/Wait calls made by that source.
func (runner *Runner) Run(ctx context.Context, request *RunRequest) (runResult RunResult, runErr error) {
	if ctx == nil {
		return RunResult{}, oops.In("workflow").Code("invalid_context").Errorf("context is required")
	}

	if request == nil {
		return RunResult{}, oops.In("workflow").Code("invalid_request").Errorf("request is required")
	}

	run := &runHost{
		runID: request.RunID, ownerSessionID: request.OwnerSessionID, controller: runner.controller,
		onEvent: request.OnEvent, launched: make(map[string]struct{}), taskIDs: make([]string, 0),
		invocations: make(map[string]int), persisted: make(map[invocationKey]persistedInvocation),
		mu: sync.Mutex{}, launchMu: sync.Mutex{}, eventMu: sync.Mutex{},
	}
	for _, link := range request.PersistedLinks {
		key := invocationKey{nodeKey: normalizeNodeKey(link.NodeKey), index: link.InvocationIndex}

		run.persisted[key] = persistedInvocation{taskID: link.AgentTaskID}

		run.launched[link.AgentTaskID] = struct{}{}
		run.taskIDs = append(run.taskIDs, link.AgentTaskID)
	}

	client := executeworker.Client{Executable: runner.executable, Handler: run.handleRPC}
	result, err := client.EvalRequest(ctx, &executeworker.Request{
		Mode: "workflow", Name: request.Name, Source: request.Source, Arguments: request.Arguments,
	})

	value := normalizeWorkflowValue(result.Value)
	if result.ValueKind == pipelineResultKind {
		value = normalizePipelineResults(value)
	}

	if err != nil {
		runErr = errors.Join(runErr, oops.In("workflow").Code("evaluate_source").Wrapf(err, "evaluate workflow source"))
		runErr = errors.Join(runErr, run.cancelActive(ctx))
	}

	taskResults, snapshotErr := run.taskResults(ctx)
	runResult = RunResult{
		Value: value, Stdout: result.Stdout, Stderr: result.Stderr,
		LaunchedTaskIDs: run.launchedTaskIDs(), TaskResults: taskResults,
	}

	if snapshotErr != nil {
		runErr = errors.Join(runErr, oops.In("workflow").Code("snapshot_tasks").
			Wrapf(snapshotErr, "snapshot workflow tasks"))
		if err == nil {
			runErr = errors.Join(runErr, run.cancelActive(ctx))
		}
	}

	if runErr != nil {
		return runResult, runErr
	}

	return runResult, nil
}

func normalizeWorkflowValue(value any) any {
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}

	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()

	var normalized any
	if err := decoder.Decode(&normalized); err != nil {
		return value
	}

	return normalizeNumbers(normalized)
}

func normalizePipelineResults(value any) any {
	items, ok := value.([]any)
	if !ok {
		return value
	}

	if len(items) == 0 {
		return []PipelineResult{}
	}

	results := make([]PipelineResult, len(items))
	for index, item := range items {
		fields, fieldsOK := item.(map[string]any)
		if !fieldsOK || len(fields) != 3 {
			return value
		}

		result, ok := pipelineResultFromFields(fields)
		if !ok {
			return value
		}

		results[index] = result
		if results[index].Value == nil && results[index].Error != pipelineNotScheduled {
			results[index].Value = TaskResult{
				ID: "", State: "", Result: "", ErrorCode: "", ErrorMessage: "",
			}
		}
	}

	return results
}

func pipelineResultFromFields(fields map[string]any) (PipelineResult, bool) {
	itemIndex, indexOK := fields["index"].(int)
	itemValue, valueOK := fields["value"]
	itemError, errorOK := fields["error"].(string)

	return PipelineResult{Index: itemIndex, Value: itemValue, Error: itemError}, indexOK && valueOK && errorOK
}

func normalizeNumbers(value any) any {
	switch typed := value.(type) {
	case json.Number:
		if integer, err := typed.Int64(); err == nil {
			return int(integer)
		}

		decimal, err := typed.Float64()
		if err != nil {
			return typed.String()
		}

		return decimal
	case []any:
		for index := range typed {
			typed[index] = normalizeNumbers(typed[index])
		}
	case map[string]any:
		for key := range typed {
			typed[key] = normalizeNumbers(typed[key])
		}
	}

	return value
}

type invocationKey struct {
	nodeKey string
	index   int
}

type persistedInvocation struct {
	taskID string
}

type workflowAgentRPCInput struct {
	Prompt  string         `json:"prompt"`
	Options []AgentOptions `json:"options"`
}

type runHost struct {
	controller     Controller
	runID          string
	onEvent        EventSink
	ownerSessionID string
	launched       map[string]struct{}
	invocations    map[string]int
	persisted      map[invocationKey]persistedInvocation
	taskIDs        []string
	mu             sync.Mutex
	launchMu       sync.Mutex
	eventMu        sync.Mutex
}

func (host *runHost) handleRPC(ctx context.Context, message *executeworker.Message) (any, error) {
	switch message.Method {
	case "workflow_agent":
		var input workflowAgentRPCInput
		if err := json.Unmarshal(message.Input, &input); err != nil {
			return nil, oops.In("workflow").Code("invalid_agent_rpc").Wrapf(err, "decode agent request")
		}

		return host.agent(ctx, input.Prompt, input.Options...)
	case "workflow_wait":
		return host.wait(ctx, message.Name)
	case "workflow_list":
		return host.list(ctx)
	case "workflow_cancel":
		return host.cancel(ctx, message.Name)
	default:
		return nil, oops.In("workflow").Code("invalid_rpc_method").Errorf("unknown workflow RPC %q", message.Method)
	}
}

func (host *runHost) agent(ctx context.Context, prompt string, options ...AgentOptions) (string, error) {
	// A pipeline may invoke Agent concurrently. Keep invocation allocation,
	// persisted-link replay, submission, and launch publication in one stable
	// order so a later invocation cannot be persisted before an earlier one.
	host.launchMu.Lock()
	defer host.launchMu.Unlock()

	if err := ctx.Err(); err != nil {
		return "", oops.In("workflow").Code("run_canceled").Wrapf(err, "launch agent")
	}

	agentOptions, nodeKey, err := validatedAgentInput(prompt, options)
	if err != nil {
		return "", err
	}

	host.mu.Lock()
	invocationIndex := host.invocations[nodeKey]
	host.invocations[nodeKey] = invocationIndex + 1
	host.mu.Unlock()

	if reusedTaskID, reused, reuseErr := host.persistedTask(ctx, nodeKey, invocationIndex); reuseErr != nil || reused {
		return reusedTaskID, reuseErr
	}

	task, err := host.controller.Submit(ctx, &AgentRequest{
		ParentTaskID:    host.runID,
		OwnerSessionID:  host.ownerSessionID,
		NodeKey:         nodeKey,
		InvocationIndex: invocationIndex,
		Prompt:          prompt,
		Options:         agentOptions,
	})
	if err != nil {
		return "", oops.In("workflow").Code("submit_agent").Wrapf(err, "submit agent task")
	}

	if task == nil || task.Task.ID == "" {
		return "", oops.In("workflow").Code("invalid_agent_task").Errorf("controller returned an empty task")
	}

	if task.Task.OwnerSessionID != host.ownerSessionID {
		return "", oops.In("workflow").Code("owner_mismatch").
			Errorf("controller returned a task owned by another session")
	}

	host.mu.Lock()
	host.launched[task.Task.ID] = struct{}{}
	host.taskIDs = append(host.taskIDs, task.Task.ID)
	host.mu.Unlock()

	if err := host.emit(ctx, &Event{
		Kind: EventTaskLaunched, TaskID: task.Task.ID, NodeKey: nodeKey,
		InvocationIndex: invocationIndex, Task: taskResult(task),
	}); err != nil {
		return "", err
	}

	return task.Task.ID, nil
}

func (host *runHost) wait(ctx context.Context, taskID string) (TaskResult, error) {
	if !host.owns(taskID) {
		return TaskResult{}, oops.In("workflow").Code("task_not_owned").Errorf("task was not launched by this workflow")
	}

	task, found, err := host.controller.Get(ctx, taskID)
	if err != nil {
		return TaskResult{}, oops.In("workflow").Code("get_task").Wrapf(err, "get agent task")
	}

	if !found || task.Task.OwnerSessionID != host.ownerSessionID {
		return TaskResult{}, oops.In("workflow").Code("task_not_owned").
			Errorf(taskNotOwnedBySession)
	}

	task, err = host.controller.Await(ctx, taskID)
	if err != nil {
		return TaskResult{}, oops.In("workflow").Code("await_task").Wrapf(err, "await agent task")
	}

	if task == nil || task.Task.OwnerSessionID != host.ownerSessionID {
		return TaskResult{}, oops.In("workflow").Code("task_not_owned").
			Errorf("controller returned a task owned by another session")
	}

	result := taskResult(task)
	if err := host.emit(ctx, &Event{
		Task: result, Kind: EventTaskCompleted, TaskID: taskID, NodeKey: "", InvocationIndex: 0,
	}); err != nil {
		return TaskResult{}, err
	}

	return result, nil
}

func (host *runHost) list(ctx context.Context) ([]TaskResult, error) {
	taskIDs := host.launchedTaskIDs()

	results := make([]TaskResult, 0, len(taskIDs))
	for _, taskID := range taskIDs {
		task, found, err := host.controller.Get(ctx, taskID)
		if err != nil {
			return nil, oops.In("workflow").Code("list_task").Wrapf(err, "get launched task %s", taskID)
		}

		if !found || task == nil || task.Task.OwnerSessionID != host.ownerSessionID {
			return nil, oops.In("workflow").Code("task_not_owned").
				Errorf(taskNotOwnedBySession)
		}

		results = append(results, taskResult(task))
	}

	return results, nil
}

func (host *runHost) cancel(ctx context.Context, taskID string) (TaskResult, error) {
	if !host.owns(taskID) {
		return TaskResult{}, oops.In("workflow").Code("task_not_owned").
			Errorf("task was not launched by this workflow")
	}

	task, found, err := host.controller.Get(ctx, taskID)
	if err != nil {
		return TaskResult{}, oops.In("workflow").Code("get_task").Wrapf(err, "get agent task")
	}

	if !found || task == nil || task.Task.OwnerSessionID != host.ownerSessionID {
		return TaskResult{}, oops.In("workflow").Code("task_not_owned").
			Errorf(taskNotOwnedBySession)
	}

	canceled, found, err := host.controller.Cancel(ctx, host.ownerSessionID, taskID)
	if err != nil {
		return TaskResult{}, oops.In("workflow").Code("cancel_task").Wrapf(err, "cancel agent task")
	}

	if !found || canceled == nil || canceled.OwnerSessionID != host.ownerSessionID {
		return TaskResult{}, oops.In("workflow").Code("task_not_owned").
			Errorf(taskNotOwnedBySession)
	}

	return taskResultFromTask(canceled), nil
}

func validatedAgentInput(prompt string, options []AgentOptions) (AgentOptions, string, error) {
	if prompt == "" {
		return AgentOptions{}, "", oops.In("workflow").Code("invalid_agent_prompt").
			Errorf("agent prompt is required")
	}

	agentOptions, err := oneAgentOptions(options)
	if err != nil {
		return AgentOptions{}, "", err
	}

	return agentOptions, normalizeNodeKey(agentOptions.NodeKey), nil
}

func normalizeNodeKey(nodeKey string) string {
	nodeKey = strings.TrimSpace(nodeKey)
	if nodeKey == "" {
		return "agent"
	}

	return nodeKey
}

func oneAgentOptions(options []AgentOptions) (AgentOptions, error) {
	switch len(options) {
	case 0:
		return AgentOptions{
			NodeKey: "", AgentName: "", Model: "", Provider: "", ConcurrencyKey: "", Depth: 0,
		}, nil
	case 1:
		return options[0], nil
	default:
		return AgentOptions{}, oops.In("workflow").Code("invalid_agent_options").
			Errorf("agent accepts at most one options value")
	}
}

func (host *runHost) persistedTask(
	ctx context.Context,
	nodeKey string,
	invocationIndex int,
) (taskID string, found bool, err error) {
	key := invocationKey{nodeKey: normalizeNodeKey(nodeKey), index: invocationIndex}

	host.mu.Lock()
	invocation, persisted := host.persisted[key]
	host.mu.Unlock()

	if !persisted {
		return "", false, nil
	}

	task, found, err := host.controller.Get(ctx, invocation.taskID)
	if err != nil {
		return "", false, oops.In("workflow").Code("get_persisted_task").
			Wrapf(err, "get persisted agent task")
	}

	if !found || task == nil {
		return "", false, nil
	}

	host.mu.Lock()
	current, persisted := host.persisted[key]

	if !persisted || current != invocation {
		host.mu.Unlock()

		return "", false, nil
	}

	delete(host.persisted, key)
	host.mu.Unlock()

	return invocation.taskID, true, nil
}

func (host *runHost) owns(taskID string) bool {
	host.mu.Lock()
	defer host.mu.Unlock()

	_, found := host.launched[taskID]

	return found
}

func (host *runHost) launchedTaskIDs() []string {
	host.mu.Lock()
	defer host.mu.Unlock()

	return append([]string(nil), host.taskIDs...)
}

func (host *runHost) taskResults(ctx context.Context) ([]TaskResult, error) {
	return host.list(context.WithoutCancel(ctx))
}

func (host *runHost) emit(ctx context.Context, event *Event) error {
	if host.onEvent == nil {
		return nil
	}

	host.eventMu.Lock()
	defer host.eventMu.Unlock()

	if err := host.onEvent(ctx, *event); err != nil {
		return oops.In("workflow").Code("emit_event").Wrapf(err, "emit workflow event")
	}

	return nil
}

func (host *runHost) cancelActive(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cancelTimeout)
	defer cancel()

	var cancelErr error

	for _, taskID := range host.launchedTaskIDs() {
		task, found, err := host.controller.Get(ctx, taskID)
		if err != nil {
			wrapped := oops.In("workflow").Code("get_task_for_cancel").
				Wrapf(err, "get launched task %s", taskID)
			cancelErr = errors.Join(cancelErr, wrapped)

			continue
		}

		if !found || task == nil || terminal(task.Task.State) {
			continue
		}

		if _, _, err := host.controller.Cancel(ctx, host.ownerSessionID, taskID); err != nil {
			wrapped := oops.In("workflow").Code("cancel_task").
				Wrapf(err, "cancel launched task %s", taskID)
			cancelErr = errors.Join(cancelErr, wrapped)
		}
	}

	return cancelErr
}

func taskResult(task *database.AgentTaskEntity) TaskResult {
	return taskResultFromTask(&task.Task)
}

func taskResultFromTask(task *database.TaskEntity) TaskResult {
	return TaskResult{
		ID: task.ID, State: string(task.State), Result: task.Result,
		ErrorCode: task.ErrorCode, ErrorMessage: task.ErrorMessage,
	}
}

func terminal(state database.TaskState) bool {
	switch state {
	case database.TaskSucceeded, database.TaskFailed, database.TaskCanceled, database.TaskInterrupted:
		return true
	case database.TaskQueued, database.TaskRunning, database.TaskCanceling:
		return false
	default:
		return false
	}
}
