package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/executeworker"
	"github.com/omarluq/librecode/internal/mvmhost"
	"github.com/omarluq/librecode/internal/tool"
	"github.com/omarluq/librecode/internal/tooltask"
)

const executeToolName tool.Name = "execute"

const (
	executeNameKey        = "name"
	executeResultValueKey = "result_value"
	executeCallMethod     = "call"
)

type nestedToolInvoker func(context.Context, string, tool.Arguments, string) (tool.Result, ToolEvent)

type executeToolExecutor struct {
	registry *tool.Registry
	invoke   nestedToolInvoker
}

type executeToolCallResult = executeworker.ToolCallResult

func newExecuteTool(registry *tool.Registry, invoke nestedToolInvoker) *executeToolExecutor {
	return &executeToolExecutor{registry: registry, invoke: invoke}
}

func (executor *executeToolExecutor) Definition() tool.Definition {
	return tool.Definition{
		Schema: mustToolSchema(`{
			"type":"object",
			"additionalProperties":false,
			"properties":{
				"source":{
					"type":"string",
					"description":"Go source to evaluate. Imports do not select the execution profile."
				},
				"profile":{
					"type":"string",
					"enum":["turn","durable"],
					"default":"turn",
					"description":"Execution guarantees; omitted defaults to turn."
				},
				"name":{
					"type":"string",
					"description":"Optional durable display name, limited to 80 UTF-8 bytes; not execution identity."
				},
				"arguments":{
					"type":"object",
					"description":"Optional durable JSON values; canonical encoding is limited to 65536 bytes."
				},
				"limits":{
					"type":"object",
					"additionalProperties":false,
					"maxProperties":0,
					"description":"Reserved; no execution limits are accepted initially."
				},
				"output_schema":{
					"type":"object",
					"description":"Reserved structured-output schema; not supported initially."
				}
			},
			"required":["source"]
		}`),
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

func executeRequestProfile(args *executeToolInput) MVMExecutionProfile {
	if args.Profile == "" {
		return MVMExecutionProfileTurn
	}

	return args.Profile
}

func (executor *executeToolExecutor) Execute(ctx context.Context, input tool.Arguments) (tool.Result, error) {
	args, decodeErr := decodeExecuteToolInput(input, false)
	profile := executeRequestProfile(&args)

	if executor.registry == nil {
		return tool.TextResult("", executionResultDetails(nil, profile, ExecutionResultRejected)),
			oops.In("assistant").Code("execute_registry_missing").
				Errorf("execute tool registry is not configured")
	}

	if decodeErr != nil {
		return tool.TextResult("", executionResultDetails(nil, args.Profile, ExecutionResultRejected)), decodeErr
	}

	client := executeworker.Client{Executable: "", Handler: executor.handleWorkerMessage}

	result, err := client.Eval(ctx, args.Source)
	if err != nil {
		wrapped := oops.In("assistant").Code("execute_source").Wrapf(err, "execute MVM source")

		kind := ExecutionResultFailed
		if errors.Is(err, context.Canceled) {
			kind = ExecutionResultCanceled
		} else if errors.Is(err, context.DeadlineExceeded) {
			kind = ExecutionResultTimedOut
		}

		return tool.TextResult("", executeResultDetails(result, kind)), wrapped
	}

	if nested, ok := result.Value.(executeworker.ToolCallResult); ok && !nested.IsError {
		toolResult := tool.Result{Details: nested.Details, Content: nested.Content}
		if toolResult.Details == nil {
			toolResult.Details = map[string]any{}
		}

		toolResult.Details = executionResultDetails(map[string]any{
			executeResultValueKey: nil,
			"stdout":              result.Stdout,
			"stderr":              result.Stderr,
			"content":             nested.Content,
			"tool_details":        nested.Details,
		}, MVMExecutionProfileTurn, ExecutionResultCompleted)

		return boundProviderVisibleExecutionResult(toolResult), nil
	}

	text, err := executeResultText(result)
	if err != nil {
		return tool.TextResult("", executeResultDetails(result, ExecutionResultFailed)), err
	}

	return boundProviderVisibleExecutionResult(
		tool.TextResult(text, executeResultDetails(result, ExecutionResultCompleted)),
	), nil
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
		if executeHiddenTool(definition.Name) {
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
		if executeHiddenTool(definition.Name) {
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
	if executeHiddenTool(tool.Name(name)) {
		message := "execute cannot call " + name
		if tool.Name(name) == executeToolName {
			message = "execute cannot call itself"
		}

		return executeToolCallResult{
			Details: map[string]any{}, Content: nil, Error: message, IsError: true,
		}
	}

	arguments, err := tool.ArgumentsFromRaw(encoded)
	if err != nil {
		return executeToolCallResult{Details: map[string]any{}, Content: nil, Error: err.Error(), IsError: true}
	}

	if arguments.HasField(backgroundKey) {
		return executeToolCallResult{
			Details: map[string]any{}, Content: nil,
			Error: "execute cannot manage background tool calls", IsError: true,
		}
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

func executeHiddenTool(name tool.Name) bool {
	switch name {
	case executeToolName, workflowToolName:
		return true
	case tool.NameRead, tool.NameBash, tool.NameEdit, tool.NameWrite,
		tool.NameGrep, tool.NameFind, tool.NameLS, tool.NameAST, tool.NameFetch:
		return false
	}

	return false
}

func executeDefinitionMap(definition *tool.Definition) map[string]any {
	var schema any
	if raw := definition.Schema.RawMessage(); len(raw) > 0 {
		if err := json.Unmarshal(raw, &schema); err != nil {
			schema = string(raw)
		}
	}

	if tooltask.Eligible(definition.Name) {
		if wrapped, ok := schema.(map[string]any); ok {
			if variants, variantsOK := wrapped["oneOf"].([]any); variantsOK && len(variants) > 0 {
				schema = variants[0]
			}
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

	text, _, _, _, err := encodeProviderVisibleExecutionValue(result.Value)

	return text, err
}

func executeResultDetails(result mvmhost.Result, kind ExecutionResultKind) map[string]any {
	_, resultValue, partialValue, truncation, err := encodeProviderVisibleExecutionValue(result.Value)
	if err != nil {
		resultValue = result.Value
	}

	details := map[string]any{executeResultValueKey: resultValue, "stdout": result.Stdout, "stderr": result.Stderr}
	if partialValue != nil {
		details["partial_value"] = partialValue
	}

	if truncation != nil {
		details["truncation"] = truncation
	}

	return executionResultDetails(details, MVMExecutionProfileTurn, kind)
}

var _ tool.Executor = (*executeToolExecutor)(nil)
