package agenttask

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/agent"
	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/outputschema"
)

const (
	maxOutputAttempts = 3
	unknownUsageJSON  = `{"reported":false}`
)

// RuntimeRunner executes durable tasks through the shared assistant runtime.
type RuntimeRunner struct {
	runtime  *assistant.Runtime
	catalog  *agent.Catalog
	sessions *database.SessionRepository
	tasks    *database.AgentTaskRepository
}

// NewRuntimeRunner creates an assistant runtime adapter for durable agent tasks.
func NewRuntimeRunner(
	runtime *assistant.Runtime,
	catalog *agent.Catalog,
	sessions *database.SessionRepository,
	tasks *database.AgentTaskRepository,
) (*RuntimeRunner, error) {
	if runtime == nil || catalog == nil || sessions == nil || tasks == nil {
		return nil, oops.In("agenttask").Code("invalid_dependencies").
			Errorf("runtime, agent catalog, sessions, and agent tasks are required")
	}

	return &RuntimeRunner{runtime: runtime, catalog: catalog, sessions: sessions, tasks: tasks}, nil
}

// Run executes one task using the persisted agent definition and child session.
func (runner *RuntimeRunner) Run(
	ctx context.Context,
	task *database.AgentTaskEntity,
	sink EventSink,
) (Result, error) {
	definition, err := runner.taskDefinition(task)
	if err != nil {
		return Result{Text: "", UsageJSON: unknownUsageJSON}, err
	}

	session, found, err := runner.sessions.GetSession(ctx, task.ChildSessionID)
	if err != nil {
		return Result{Text: "", UsageJSON: unknownUsageJSON}, oops.In("agenttask").Code("load_child_session").
			Wrapf(err, "load child session")
	}

	if !found {
		return Result{Text: "", UsageJSON: unknownUsageJSON}, oops.In("agenttask").Code("child_session_not_found").
			With("session_id", task.ChildSessionID).Errorf("child session not found")
	}

	profile := profileFromDefinition(definition, task.Depth)
	runtime := runner.runtime.WithExecutionProfile(&profile)

	var partial partialText

	events := eventSinkState{sink: sink, mu: sync.Mutex{}, fail: nil}
	metrics := new(assistant.RunMetrics)
	runCtx := assistant.WithRunMetrics(ctx, metrics)

	metrics.SetUsageTotalsObserver(func(usageTotals model.UsageTotals) {
		events.emit(runCtx, string(assistant.StreamEventUsageTotal), usageTotals)
	})

	contract, hasContract, err := restoreOutputContract(task)
	if err != nil {
		return Result{Text: "", UsageJSON: unknownUsageJSON}, err
	}

	execution := runtimeExecution{
		runtime: runtime, task: task, cwd: session.CWD, contract: contract, hasContract: hasContract,
		metrics: metrics, events: &events, partial: &partial,
	}

	response, promptErr, runErr := runner.runPrompts(runCtx, &execution)
	if runErr != nil {
		return Result{Text: "", UsageJSON: mustUsageJSON(metrics)}, runErr
	}

	return runtimeRunResult(response, promptErr, events.err(), metrics, partial.text())
}

func restoreOutputContract(task *database.AgentTaskEntity) (*outputschema.Contract, bool, error) {
	if task.OutputSchemaJSON == "" {
		return nil, false, nil
	}

	// Existing task rows were admitted under the frozen V1 policy. Restore under that
	// policy rather than today's admission rules; canonical bytes and digest still bind identity.
	contract, err := outputschema.RestoreWithPolicy(
		[]byte(task.OutputSchemaJSON), task.OutputSchemaDigest, outputschema.PersistedPolicyV1,
	)
	if err != nil {
		return nil, false, oops.In("agenttask").Code("restore_output_schema").Wrapf(err, "restore output schema")
	}

	return contract, true, nil
}

type runtimeExecution struct {
	runtime     *assistant.Runtime
	task        *database.AgentTaskEntity
	contract    *outputschema.Contract
	metrics     *assistant.RunMetrics
	events      *eventSinkState
	partial     *partialText
	cwd         string
	hasContract bool
}

func (runner *RuntimeRunner) runPrompts(
	ctx context.Context,
	execution *runtimeExecution,
) (*assistant.PromptResponse, error, error) {
	prompt := execution.task.Prompt

	for attempt := execution.task.OutputAttemptsReserved + 1; attempt <= maxOutputAttempts; attempt++ {
		if err := runner.reserveOutputAttempt(ctx, execution.task, execution.hasContract); err != nil {
			return &assistant.PromptResponse{}, nil, err
		}

		response, promptErr := execution.runtime.Prompt(ctx, &assistant.PromptRequest{
			OnEvent: func(event assistant.StreamEvent) {
				execution.metrics.ObserveStreamEvent(event)
				execution.partial.observe(event)
				execution.events.emit(ctx, string(event.Kind), event)
			},
			OnRetry: nil, OnUserEntry: nil, OnSteeringReturn: nil, ParentEntryID: nil,
			SessionID: execution.task.ChildSessionID, CWD: execution.cwd, Text: prompt,
			Images: nil, Name: "", ResumeLatest: false, HideUserPrompt: attempt > 1,
		})
		if promptErr != nil || execution.events.err() != nil || !execution.hasContract {
			if promptErr != nil {
				promptErr = oops.In("agenttask").Code("prompt").Wrapf(promptErr, "prompt agent runtime")
			}

			return response, promptErr, nil
		}

		canonical, validationErr := execution.contract.Validate(response.Text)
		if err := runner.checkpointOutputAttempt(ctx, execution.task, execution.metrics, validationErr); err != nil {
			return &assistant.PromptResponse{}, nil, err
		}

		if validationErr == nil {
			response.Text = string(canonical)

			return response, nil, nil
		}

		if attempt == maxOutputAttempts {
			return &assistant.PromptResponse{}, nil, oops.In("agenttask").Code("validate_output").
				Wrapf(validationErr, "validate structured output")
		}

		prompt = "Return only one JSON value matching the requested output schema. Validation failed: " +
			validationErr.Error()
	}

	return &assistant.PromptResponse{}, nil, oops.In("agenttask").Code("output_schema_attempt_outcome_unknown").
		Errorf("structured output attempt budget exhausted with an unavailable outcome")
}

func (runner *RuntimeRunner) reserveOutputAttempt(
	ctx context.Context,
	task *database.AgentTaskEntity,
	hasContract bool,
) error {
	if !hasContract {
		return nil
	}

	if runner.tasks == nil {
		return oops.In("agenttask").Code("output_attempt_repository_missing").
			Errorf("output attempt repository is required")
	}

	reserved, err := runner.tasks.ReserveOutputAttempt(ctx, task.Task.ID, task.Task.LeaseOwner, maxOutputAttempts)
	if err != nil {
		return oops.In("agenttask").Code("reserve_output_attempt").Wrapf(err, "reserve output attempt")
	}

	if !reserved {
		return oops.In("agenttask").Code("output_schema_attempt_outcome_unknown").
			Errorf("structured output attempt budget exhausted with an unavailable outcome")
	}

	return nil
}

func (runner *RuntimeRunner) checkpointOutputAttempt(
	ctx context.Context,
	task *database.AgentTaskEntity,
	metrics *assistant.RunMetrics,
	validationErr error,
) error {
	summary := "valid"
	if validationErr != nil {
		summary = validationErr.Error()
	}

	err := runner.tasks.CheckpointOutputAttempt(
		ctx, task.Task.ID, task.Task.LeaseOwner, mustUsageJSON(metrics), summary,
	)
	if err != nil {
		return oops.In("agenttask").Code("checkpoint_output_attempt").Wrapf(err, "checkpoint output attempt")
	}

	return nil
}

type eventSinkState struct {
	sink EventSink
	fail error
	mu   sync.Mutex
}

func (state *eventSinkState) emit(ctx context.Context, kind string, event any) {
	state.mu.Lock()
	defer state.mu.Unlock()

	if state.fail == nil && state.sink != nil {
		state.fail = state.sink(ctx, kind, event)
	}
}

func (state *eventSinkState) err() error {
	state.mu.Lock()
	defer state.mu.Unlock()

	return state.fail
}

// partialText accumulates streamed assistant text so failed or canceled runs
// can surface the output produced before the error. Events may arrive from
// provider streaming callbacks, so access is synchronized.
type partialText struct {
	sb strings.Builder
	mu sync.Mutex
}

func (partial *partialText) observe(event assistant.StreamEvent) {
	if event.Kind != assistant.StreamEventTextDelta || event.Text == "" {
		return
	}

	partial.mu.Lock()
	defer partial.mu.Unlock()

	partial.sb.WriteString(event.Text)
}

func (partial *partialText) text() string {
	partial.mu.Lock()
	defer partial.mu.Unlock()

	return partial.sb.String()
}

func runtimeRunResult(
	response *assistant.PromptResponse,
	promptErr error,
	eventErr error,
	metrics *assistant.RunMetrics,
	partialText string,
) (Result, error) {
	usageJSON, usageErr := agentUsageJSON(metrics)
	if promptErr != nil {
		if usageErr != nil {
			usageJSON = unknownUsageJSON
		}

		return Result{Text: partialText, UsageJSON: usageJSON},
			oops.In("agenttask").Code("run_prompt").Wrapf(promptErr, "run agent prompt")
	}

	if eventErr != nil {
		return Result{Text: response.Text, UsageJSON: usageJSON}, eventErr
	}

	if usageErr != nil {
		return Result{Text: response.Text, UsageJSON: unknownUsageJSON}, usageErr
	}

	return Result{Text: response.Text, UsageJSON: usageJSON}, nil
}

func mustUsageJSON(metrics *assistant.RunMetrics) string {
	usage, err := agentUsageJSON(metrics)
	if err != nil {
		return unknownUsageJSON
	}

	return usage
}

func agentUsageJSON(metrics *assistant.RunMetrics) (string, error) {
	usageTotals, err := metrics.UsageTotals()
	if err != nil {
		return unknownUsageJSON, oops.In("agenttask").Code("invalid_usage").Wrapf(err, "collect agent usage")
	}

	encoded, err := json.Marshal(usageTotals)
	if err != nil {
		return unknownUsageJSON, oops.In("agenttask").Code("marshal_usage").Wrapf(err, "marshal agent usage")
	}

	return string(encoded), nil
}

func (runner *RuntimeRunner) taskDefinition(task *database.AgentTaskEntity) (*agent.Definition, error) {
	if task.PolicyJSON != "" && task.PolicyJSON != "{}" {
		var definition agent.Definition
		if err := json.Unmarshal([]byte(task.PolicyJSON), &definition); err != nil {
			return nil, oops.In("agenttask").Code("decode_agent_profile").Wrapf(err, "decode agent profile")
		}

		return &definition, nil
	}

	definition, found := runner.catalog.Get(task.AgentName)
	if !found {
		return nil, oops.In("agenttask").Code("agent_not_found").
			With("agent", task.AgentName).Errorf("agent definition not found")
	}

	return &definition, nil
}

func profileFromDefinition(definition *agent.Definition, depth int) assistant.ExecutionProfile {
	return assistant.ExecutionProfile{
		Kind: assistant.ExecutionAgentTask, AgentName: definition.Name,
		SystemPrompt: definition.SystemPrompt, Provider: definition.Model.Provider,
		Model: definition.Model.Model, ThinkingLevel: definition.Model.Thinking,
		PermissionMode: definition.Permissions, Tools: definition.Tools,
		EnableSkills: false, EnableExtensions: false,
		MaxTurns: 0, Depth: depth,
	}
}
