package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/agent"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/tool"
)

const (
	agentStartToolName       tool.Name = "agent_start"
	agentStatusToolName      tool.Name = "agent_status"
	agentWaitToolName        tool.Name = "agent_wait"
	agentCancelToolName      tool.Name = "agent_cancel"
	agentListToolName        tool.Name = "agent_list"
	maxAgentPromptBytes                = 32 * 1024
	maxChildSessionNameRunes           = 80
	defaultAgentListLimit              = 20
	maxAgentListLimit                  = 100
)

// AgentTaskRequest describes one asynchronous agent submission.
type AgentTaskRequest struct {
	OwnerSessionID string
	ChildSessionID string
	AgentName      string
	Prompt         string
	Model          string
	Provider       string
	PolicyJSON     string
	Depth          int
}

// AgentTaskController is the runtime-facing boundary for durable agent work.
type AgentTaskController interface {
	SubmitAgentTask(context.Context, *AgentTaskRequest) (*database.AgentTaskEntity, error)
	Get(context.Context, string) (*database.AgentTaskEntity, bool, error)
	List(context.Context, string, int) ([]database.TaskEntity, error)
	Cancel(context.Context, string, string) (*database.TaskEntity, bool, error)
	Await(context.Context, string) (*database.AgentTaskEntity, error)
	SubscribeAgentTask(string) (events <-chan database.TaskEventEntity, cancel func())
}

type agentToolExecutor struct {
	controller      AgentTaskController
	sessions        *database.SessionRepository
	catalog         *agent.Catalog
	name            tool.Name
	parentSessionID string
	cwd             string
}

type agentStartInput struct {
	Agent  string `json:"agent"`
	Prompt string `json:"prompt"`
}

type agentTaskInput struct {
	TaskID string `json:"task_id"`
}

type agentWaitInput struct {
	TaskID string `json:"task_id"`
}

type agentListInput struct {
	Limit int `json:"limit,omitempty"`
}

//nolint:lll // JSON schemas are kept as single validated literals.
func (executor *agentToolExecutor) Definition() tool.Definition {
	definitions := map[tool.Name]tool.Definition{
		agentStartToolName: {
			Schema: schemaForAgentStart(executor.catalog), Name: agentStartToolName, Label: "Start agent",
			Description:   "Start a durable asynchronous subagent and return its task ID immediately.",
			PromptSnippet: "Start an asynchronous subagent", PromptGuidelines: []string{
				"Delegate focused independent work, then use agent_wait or agent_status to collect it.",
			}, ReadOnly: false,
		},
		agentStatusToolName: taskToolDefinition(agentStatusToolName, "Get agent task status", "Inspect an asynchronous agent task."),
		agentWaitToolName: {
			Schema: mustToolSchema(`{"type":"object","additionalProperties":false,"properties":{"task_id":{"type":"string"}},"required":["task_id"]}`),
			Name:   agentWaitToolName, Label: "Check agent", Description: "Check an agent task without blocking the parent conversation.",
			PromptSnippet: "Check an asynchronous agent", PromptGuidelines: []string{
				"This check returns immediately. Do not poll repeatedly; continue other work or let the user interact while the agent runs.",
			}, ReadOnly: true,
		},
		agentCancelToolName: taskToolDefinition(agentCancelToolName, "Cancel agent task", "Cancel a queued or running agent task."),
		agentListToolName: {
			Schema: mustToolSchema(`{"type":"object","additionalProperties":false,"properties":{"limit":{"type":"integer","minimum":1,"maximum":100}}}`),
			Name:   agentListToolName, Label: "List agents", Description: "List asynchronous agent tasks owned by this session.",
			PromptSnippet: "List asynchronous agents", PromptGuidelines: []string{}, ReadOnly: true,
		},
	}

	return definitions[executor.name]
}

//nolint:lll // JSON schemas are kept as single validated literals.
func taskToolDefinition(name tool.Name, label, description string) tool.Definition {
	return tool.Definition{
		Schema: mustToolSchema(`{"type":"object","additionalProperties":false,"properties":{"task_id":{"type":"string"}},"required":["task_id"]}`),
		Name:   name, Label: label, Description: description, PromptSnippet: description,
		PromptGuidelines: []string{}, ReadOnly: name != agentCancelToolName,
	}
}

//nolint:lll // The generated JSON schema remains readable as one format string.
func schemaForAgentStart(catalog *agent.Catalog) tool.Schema {
	definitions := catalog.Definitions()

	descriptions := make([]string, 0, len(definitions))
	for index := range definitions {
		descriptions = append(descriptions, definitions[index].Name+": "+definitions[index].Description)
	}

	description := "Name of the subagent. Available agents: " + strings.Join(descriptions, "; ")

	return mustToolSchema(fmt.Sprintf(`{"type":"object","additionalProperties":false,"properties":{"agent":{"type":"string","description":%q},"prompt":{"type":"string","description":"Focused task for the subagent."}},"required":["agent","prompt"]}`, description))
}

func mustToolSchema(raw string) tool.Schema {
	schema, err := tool.SchemaFromRaw([]byte(raw))
	if err != nil {
		panic(err)
	}

	return schema
}

func (executor *agentToolExecutor) Execute(ctx context.Context, input tool.Arguments) (tool.Result, error) {
	switch executor.name { //nolint:exhaustive // This executor only handles dynamically registered agent tool names.
	case agentStartToolName:
		return executor.start(ctx, input)
	case agentStatusToolName:
		return executor.status(ctx, input)
	case agentWaitToolName:
		return executor.wait(ctx, input)
	case agentCancelToolName:
		return executor.cancel(ctx, input)
	case agentListToolName:
		return executor.list(ctx, input)
	default:
		return tool.TextResult("", nil), fmt.Errorf("unknown agent tool %q", executor.name)
	}
}

//nolint:lll // Error and constructor expressions are clearer without artificial wrapping.
func (executor *agentToolExecutor) start(ctx context.Context, input tool.Arguments) (tool.Result, error) {
	var args agentStartInput
	if err := input.Decode(&args); err != nil {
		return tool.TextResult("", nil), err
	}

	args.Agent, args.Prompt = strings.ToLower(strings.TrimSpace(args.Agent)), strings.TrimSpace(args.Prompt)
	if args.Agent == "" || args.Prompt == "" {
		return tool.TextResult("", nil), errors.New("agent and prompt are required")
	}

	if len(args.Prompt) > maxAgentPromptBytes || !utf8.ValidString(args.Prompt) {
		return tool.TextResult("", nil), fmt.Errorf("agent prompt must be valid UTF-8 and at most %d bytes", maxAgentPromptBytes)
	}

	definition, found := executor.catalog.Get(args.Agent)
	if !found {
		return tool.TextResult("", nil), fmt.Errorf("unknown agent %q", args.Agent)
	}

	task, err := executor.submit(ctx, &definition, args.Prompt)
	if err != nil {
		return tool.TextResult("", nil), err
	}

	text := fmt.Sprintf("Started %s agent task %s.", definition.Name, task.Task.ID)

	return tool.TextResult(text, agentTaskDetails(task)), nil
}

func (executor *agentToolExecutor) submit(
	ctx context.Context,
	definition *agent.Definition,
	prompt string,
) (*database.AgentTaskEntity, error) {
	policy, err := json.Marshal(definition)
	if err != nil {
		return nil, oops.In("assistant").Code("encode_agent_profile").Wrapf(err, "encode agent profile")
	}

	child, err := executor.sessions.CreateSession(
		ctx, executor.cwd, childSessionName(definition.Name, prompt), executor.parentSessionID,
	)
	if err != nil {
		return nil, oops.In("assistant").Code("create_agent_session").Wrapf(err, "create child agent session")
	}

	task, err := executor.controller.SubmitAgentTask(ctx, &AgentTaskRequest{
		OwnerSessionID: executor.parentSessionID, ChildSessionID: child.ID, AgentName: definition.Name,
		Prompt: prompt, Model: definition.Model.Model, Provider: definition.Model.Provider,
		PolicyJSON: string(policy), Depth: 1,
	})
	if err == nil {
		return task, nil
	}

	if task != nil {
		return task, oops.In("assistant").Code("submit_agent_task").Wrapf(err, "submit agent task")
	}

	if deleteErr := executor.sessions.DeleteSession(context.WithoutCancel(ctx), child.ID); deleteErr != nil {
		return nil, errors.Join(err, deleteErr)
	}

	return nil, oops.In("assistant").Code("submit_agent_task").Wrapf(err, "submit agent task")
}

func (executor *agentToolExecutor) status(ctx context.Context, input tool.Arguments) (tool.Result, error) {
	args, err := decodeAgentTaskInput(input)
	if err != nil {
		return tool.TextResult("", nil), err
	}

	task, err := executor.ownedTask(ctx, args.TaskID)
	if err != nil {
		return tool.TextResult("", nil), err
	}

	return agentTaskResult(task), nil
}

func (executor *agentToolExecutor) wait(ctx context.Context, input tool.Arguments) (tool.Result, error) {
	var args agentWaitInput
	if err := input.Decode(&args); err != nil {
		return tool.TextResult("", nil), err
	}

	args.TaskID = strings.TrimSpace(args.TaskID)

	task, err := executor.ownedTask(ctx, args.TaskID)
	if err != nil {
		return tool.TextResult("", map[string]any{"task_id": args.TaskID}), err
	}

	return agentTaskResult(task), nil
}

func (executor *agentToolExecutor) cancel(ctx context.Context, input tool.Arguments) (tool.Result, error) {
	args, err := decodeAgentTaskInput(input)
	if err != nil {
		return tool.TextResult("", nil), err
	}

	if _, err = executor.ownedTask(ctx, args.TaskID); err != nil {
		return tool.TextResult("", nil), err
	}

	_, found, err := executor.controller.Cancel(ctx, executor.parentSessionID, args.TaskID)
	if err != nil {
		return tool.TextResult("", nil), err
	}

	if !found {
		return tool.TextResult("", nil), fmt.Errorf("agent task %q not found", args.TaskID)
	}

	task, err := executor.ownedTask(ctx, args.TaskID)
	if err != nil {
		return tool.TextResult("", nil), err
	}

	return agentTaskResult(task), nil
}

func (executor *agentToolExecutor) list(ctx context.Context, input tool.Arguments) (tool.Result, error) {
	var args agentListInput
	if err := input.Decode(&args); err != nil {
		return tool.TextResult("", nil), err
	}

	limit := args.Limit
	if limit <= 0 {
		limit = defaultAgentListLimit
	}

	if limit > maxAgentListLimit {
		limit = maxAgentListLimit
	}

	tasks, err := executor.controller.List(ctx, executor.parentSessionID, limit)
	if err != nil {
		return tool.TextResult("", nil), err
	}

	lines := make([]string, 0, len(tasks))
	for i := range tasks {
		lines = append(lines, fmt.Sprintf("%s %s", tasks[i].ID, tasks[i].State))
	}

	return tool.TextResult(strings.Join(lines, "\n"), map[string]any{"count": len(tasks)}), nil
}

func decodeAgentTaskInput(input tool.Arguments) (agentTaskInput, error) {
	var args agentTaskInput

	err := input.Decode(&args)

	args.TaskID = strings.TrimSpace(args.TaskID)
	if err == nil && args.TaskID == "" {
		err = errors.New("task_id is required")
	}

	return args, err
}

func (executor *agentToolExecutor) ownedTask(ctx context.Context, taskID string) (*database.AgentTaskEntity, error) {
	task, found, err := executor.controller.Get(ctx, taskID)
	if err != nil {
		return nil, oops.In("assistant").Code("get_agent_task").Wrapf(err, "get agent task")
	}

	if !found || task.Task.OwnerSessionID != executor.parentSessionID {
		return nil, fmt.Errorf("agent task %q not found", taskID)
	}

	return task, nil
}

func agentTaskResult(task *database.AgentTaskEntity) tool.Result {
	text := task.Task.Result
	if text == "" {
		text = fmt.Sprintf("Agent task %s is %s.", task.Task.ID, task.Task.State)
	}

	if task.Task.ErrorMessage != "" {
		text += "\n" + task.Task.ErrorMessage
	}

	return tool.TextResult(text, agentTaskDetails(task))
}

func agentTaskDetails(task *database.AgentTaskEntity) map[string]any {
	return map[string]any{
		"task_id": task.Task.ID, "state": task.Task.State, "agent": task.AgentName,
		"session_id": task.ChildSessionID, "usage": task.UsageJSON,
	}
}

func childSessionName(agentName, prompt string) string {
	name := agentName + ": " + strings.Join(strings.Fields(prompt), " ")

	runes := []rune(name)
	if len(runes) > maxChildSessionNameRunes {
		name = string(runes[:maxChildSessionNameRunes-1]) + "…"
	}

	return name
}
