package agenttask

import (
	"context"
	"encoding/json"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/agent"
	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
)

// RuntimeRunner executes durable tasks through the shared assistant runtime.
type RuntimeRunner struct {
	runtime  *assistant.Runtime
	catalog  *agent.Catalog
	sessions *database.SessionRepository
}

// NewRuntimeRunner creates an assistant runtime adapter for durable agent tasks.
func NewRuntimeRunner(
	runtime *assistant.Runtime,
	catalog *agent.Catalog,
	sessions *database.SessionRepository,
) (*RuntimeRunner, error) {
	if runtime == nil || catalog == nil || sessions == nil {
		return nil, oops.In("agenttask").Code("invalid_dependencies").
			Errorf("runtime, agent catalog, and sessions are required")
	}

	return &RuntimeRunner{runtime: runtime, catalog: catalog, sessions: sessions}, nil
}

// Run executes one task using the persisted agent definition and child session.
func (runner *RuntimeRunner) Run(
	ctx context.Context,
	task *database.AgentTaskEntity,
	sink EventSink,
) (Result, error) {
	definition, err := runner.taskDefinition(task)
	if err != nil {
		return Result{Text: "", UsageJSON: "{}"}, err
	}

	session, found, err := runner.sessions.GetSession(ctx, task.ChildSessionID)
	if err != nil {
		return Result{Text: "", UsageJSON: "{}"}, oops.In("agenttask").Code("load_child_session").
			Wrapf(err, "load child session")
	}

	if !found {
		return Result{Text: "", UsageJSON: "{}"}, oops.In("agenttask").Code("child_session_not_found").
			With("session_id", task.ChildSessionID).Errorf("child session not found")
	}

	profile := profileFromDefinition(definition, task.Depth)
	runtime := runner.runtime.WithExecutionProfile(&profile)

	var eventErr error

	response, err := runtime.Prompt(ctx, &assistant.PromptRequest{
		OnEvent: func(event assistant.StreamEvent) {
			if eventErr == nil {
				eventErr = sink(ctx, string(event.Kind), event)
			}
		},
		OnRetry: nil, OnUserEntry: nil, ParentEntryID: nil,
		SessionID: task.ChildSessionID, CWD: session.CWD, Text: task.Prompt,
		Name: "", ResumeLatest: false, HideUserPrompt: false,
	})
	if err != nil {
		return Result{Text: "", UsageJSON: "{}"}, oops.In("agenttask").Code("run_prompt").Wrapf(err, "run agent prompt")
	}

	if eventErr != nil {
		return Result{Text: response.Text, UsageJSON: "{}"}, eventErr
	}

	usage, err := json.Marshal(response.Usage)
	if err != nil {
		return Result{Text: response.Text, UsageJSON: "{}"}, oops.In("agenttask").Code("marshal_usage").
			Wrapf(err, "marshal agent usage")
	}

	return Result{Text: response.Text, UsageJSON: string(usage)}, nil
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
