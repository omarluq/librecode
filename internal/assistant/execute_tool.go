package assistant

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/executeworker"
	"github.com/omarluq/librecode/internal/mvmhost"
	"github.com/omarluq/librecode/internal/tool"
)

const executeToolName tool.Name = "execute"

const (
	defaultExecuteResultLimit = 1 << 20
	executeNameKey            = "name"
	executeResultValueKey     = "result_value"
	executeCallMethod         = "call"
)

type nestedToolInvoker func(context.Context, string, tool.Arguments, string) (tool.Result, ToolEvent)

type executeToolExecutor struct {
	registry *tool.Registry
	invoke   nestedToolInvoker
}

type executeToolInput struct {
	Source string `json:"source"`
}

type executeToolCallResult = executeworker.ToolCallResult

func newExecuteTool(registry *tool.Registry, invoke nestedToolInvoker) *executeToolExecutor {
	return &executeToolExecutor{registry: registry, invoke: invoke}
}

func (executor *executeToolExecutor) Definition() tool.Definition {
	return tool.Definition{
		Schema: mustToolSchema(
			`{"type":"object","additionalProperties":false,"properties":` +
				`{"source":{"type":"string","description":"Go source to evaluate with the tools package."}},` +
				`"required":["source"]}`,
		),
		Name:          executeToolName,
		Label:         "Execute Go",
		Description:   "Evaluate Go source that can search, describe, and call the tools available for this prompt.",
		PromptSnippet: "Use execute for compact multi-tool programs",
		PromptGuidelines: []string{
			`Import "tools" to use tools.Search(query), tools.Describe(name), and tools.Call(name, input).`,
			"The execute tool cannot search for, describe, or call itself.",
		},
		ReadOnly: false,
	}
}

func (executor *executeToolExecutor) Execute(ctx context.Context, input tool.Arguments) (tool.Result, error) {
	if executor.registry == nil {
		return tool.Result{}, oops.In("assistant").Code("execute_registry_missing").
			Errorf("execute tool registry is not configured")
	}

	var args executeToolInput
	if err := input.Decode(&args); err != nil {
		return tool.Result{}, oops.In("assistant").Code("execute_input").Wrapf(err, "decode execute input")
	}

	client := executeworker.Client{Executable: "", Handler: executor.handleWorkerMessage}

	result, err := client.Eval(ctx, args.Source)
	if err != nil {
		wrapped := oops.In("assistant").Code("execute_source").Wrapf(err, "execute MVM source")

		return tool.TextResult("", executeResultDetails(result)), wrapped
	}

	if nested, ok := result.Value.(executeworker.ToolCallResult); ok && !nested.IsError {
		toolResult := tool.Result{Details: nested.Details, Content: nested.Content}
		if toolResult.Details == nil {
			toolResult.Details = map[string]any{}
		}

		toolResult.Details["execute_stdout"] = result.Stdout
		toolResult.Details["execute_stderr"] = result.Stderr

		return toolResult, nil
	}

	text, err := executeResultText(result)
	if err != nil {
		return tool.Result{}, err
	}

	return tool.TextResult(text, executeResultDetails(result)), nil
}

func (executor *executeToolExecutor) handleWorkerMessage(
	ctx context.Context,
	message *executeworker.Message,
) (any, error) {
	switch message.Method {
	case "search":
		return executor.search(message.Query), nil
	case "describe":
		return executor.describe(message.Name), nil
	case executeCallMethod:
		if _, err := tool.ArgumentsFromRaw(message.Input); err != nil {
			return nil, oops.In("assistant").Code("execute_rpc_input").Wrapf(err, "decode nested tool input")
		}

		return executor.call(ctx, message.Name, message.Input), nil
	default:
		return nil, oops.In("assistant").Code("execute_rpc_method").Errorf(
			"unknown execute worker RPC method %q",
			message.Method,
		)
	}
}

func (executor *executeToolExecutor) search(query string) []map[string]any {
	query = strings.ToLower(strings.TrimSpace(query))
	matches := make([]map[string]any, 0)

	for _, definition := range executor.registry.Definitions() {
		if definition.Name == executeToolName || definition.Name == workflowToolName {
			continue
		}

		haystack := strings.ToLower(strings.Join([]string{
			string(definition.Name), definition.Label, definition.Description, definition.PromptSnippet,
		}, " "))
		if query == "" || strings.Contains(haystack, query) {
			matches = append(matches, executeDefinitionMap(&definition))
		}
	}

	return matches
}

func (executor *executeToolExecutor) describe(name string) map[string]any {
	for _, definition := range executor.registry.Definitions() {
		if definition.Name == executeToolName || definition.Name == workflowToolName {
			continue
		}

		if string(definition.Name) == name {
			return executeDefinitionMap(&definition)
		}
	}

	return nil
}

func (executor *executeToolExecutor) call(
	ctx context.Context,
	name string,
	encoded json.RawMessage,
) executeToolCallResult {
	if tool.Name(name) == executeToolName {
		return executeToolCallResult{
			Details: map[string]any{}, Content: nil, Error: "execute cannot call itself", IsError: true,
		}
	}

	if tool.Name(name) == workflowToolName {
		return executeToolCallResult{
			Details: map[string]any{}, Content: nil, Error: "execute cannot call workflow", IsError: true,
		}
	}

	arguments, err := tool.ArgumentsFromRaw(encoded)
	if err != nil {
		return executeToolCallResult{Details: map[string]any{}, Content: nil, Error: err.Error(), IsError: true}
	}

	var result tool.Result

	if executor.invoke != nil {
		var event ToolEvent

		result, event = executor.invoke(ctx, name, arguments, string(encoded))

		if event.IsError {
			return executeToolCallResult{
				Details: result.Details, Content: result.Content, Error: event.Error, IsError: true,
			}
		}
	} else {
		result, err = executor.registry.Execute(ctx, name, arguments)
	}

	outcome := executeToolCallResult{Details: result.Details, Content: result.Content, Error: "", IsError: false}
	if outcome.Details == nil {
		outcome.Details = map[string]any{}
	}

	if err != nil {
		outcome.Error = err.Error()
		outcome.IsError = true
	}

	return outcome
}

func executeDefinitionMap(definition *tool.Definition) map[string]any {
	var schema any
	if raw := definition.Schema.RawMessage(); len(raw) > 0 {
		if err := json.Unmarshal(raw, &schema); err != nil {
			schema = string(raw)
		}
	}

	return map[string]any{
		executeNameKey:      string(definition.Name),
		"label":             definition.Label,
		"description":       definition.Description,
		"prompt_snippet":    definition.PromptSnippet,
		"prompt_guidelines": definition.PromptGuidelines,
		"read_only":         definition.ReadOnly,
		"schema":            schema,
	}
}

func executeResultText(result mvmhost.Result) (string, error) {
	if result.Value == nil {
		if result.Stdout != "" {
			return result.Stdout, nil
		}

		return "null", nil
	}

	encoded, err := json.Marshal(result.Value)
	if err != nil {
		return "", oops.In("assistant").Code("execute_result_encode").Wrapf(err, "encode execute result")
	}

	if len(encoded) > defaultExecuteResultLimit {
		return "", oops.In("assistant").Code("execute_result_limit").Errorf(
			"encoded execute result is %d bytes; limit is %d",
			len(encoded),
			defaultExecuteResultLimit,
		)
	}

	return string(encoded), nil
}

func executeResultDetails(result mvmhost.Result) map[string]any {
	return map[string]any{executeResultValueKey: result.Value, "stdout": result.Stdout, "stderr": result.Stderr}
}

var _ tool.Executor = (*executeToolExecutor)(nil)
