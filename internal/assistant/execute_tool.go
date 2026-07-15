package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/mvmhost"
	"github.com/omarluq/librecode/internal/tool"
)

const executeToolName tool.Name = "execute"

const (
	executeNameKey        = "name"
	executeResultValueKey = "result_value"
)

type executeToolExecutor struct {
	registry  *tool.Registry
	evaluator *mvmhost.Evaluator
	invoke    func(context.Context, string, tool.Arguments, string) (tool.Result, ToolEvent)
	onNested  func(name, argumentsJSON string, result *tool.Result, event *ToolEvent)
}

type executeToolInput struct {
	Source string `json:"source"`
}

type executeToolCallResult struct {
	Details map[string]any      `json:"details"`
	Error   string              `json:"error,omitempty"`
	Content []tool.ContentBlock `json:"content"`
	IsError bool                `json:"is_error"`
}

func newExecuteTool(runtime *Runtime, registry *tool.Registry) *executeToolExecutor {
	executor := &executeToolExecutor{registry: registry, evaluator: mvmhost.New(), invoke: nil, onNested: nil}
	if runtime != nil {
		executor.invoke = func(
			ctx context.Context, name string, arguments tool.Arguments, argumentsJSON string,
		) (tool.Result, ToolEvent) {
			return runtime.invokeNestedTool(ctx, registry, name, arguments, argumentsJSON)
		}
	}

	return executor
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

	if scope := toolInvocationScopeFromContext(ctx); scope != nil {
		scope.trace = executor.onNested
	}

	bindings := mvmhost.Bindings{"tools": {
		"Search":   executor.search,
		"Describe": executor.describe,
		"Call": func(name string, input any) executeToolCallResult {
			return executor.call(ctx, name, input)
		},
	}}

	result, err := executor.evaluator.Eval(ctx, mvmhost.Request{
		Bindings: bindings, Name: "execute.go", Source: args.Source, SourceLimit: 0, OutputLimit: 0,
	})
	if err != nil {
		wrapped := oops.In("assistant").Code("execute_source").Wrapf(err, "execute MVM source")

		return tool.TextResult("", executeResultDetails(result)), wrapped
	}

	if nested, ok := result.Value.(tool.Result); ok {
		if nested.Details == nil {
			nested.Details = map[string]any{}
		}

		nested.Details["execute_stdout"] = result.Stdout
		nested.Details["execute_stderr"] = result.Stderr

		return nested, nil
	}

	text, err := executeResultText(result)
	if err != nil {
		return tool.Result{}, err
	}

	return tool.TextResult(text, executeResultDetails(result)), nil
}

func (executor *executeToolExecutor) search(query string) []map[string]any {
	query = strings.ToLower(strings.TrimSpace(query))
	matches := make([]map[string]any, 0)

	for _, definition := range executor.registry.Definitions() {
		if definition.Name == executeToolName {
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
		if definition.Name == executeToolName {
			continue
		}

		if string(definition.Name) == name {
			return executeDefinitionMap(&definition)
		}
	}

	return nil
}

func (executor *executeToolExecutor) call(ctx context.Context, name string, input any) executeToolCallResult {
	if tool.Name(name) == executeToolName {
		return executeToolCallResult{
			Details: map[string]any{}, Content: nil, Error: "execute cannot call itself", IsError: true,
		}
	}

	encoded, err := json.Marshal(input)
	if err != nil {
		return executeToolCallResult{
			Details: map[string]any{}, Content: nil, Error: fmt.Sprintf("encode tool input: %v", err), IsError: true,
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
		return result.Stdout, nil
	}

	encoded, err := json.Marshal(result.Value)
	if err != nil {
		return "", oops.In("assistant").Code("execute_result_encode").Wrapf(err, "encode execute result")
	}

	return string(encoded), nil
}

func executeResultDetails(result mvmhost.Result) map[string]any {
	return map[string]any{executeResultValueKey: result.Value, "stdout": result.Stdout, "stderr": result.Stderr}
}

var _ tool.Executor = (*executeToolExecutor)(nil)
