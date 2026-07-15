package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/tool"
)

const executeOuterCallID = "outer"

type executeTestTool struct{}

func (executor *executeTestTool) Definition() tool.Definition {
	return tool.Definition{
		Schema: mustToolSchema(
			`{"type":"object","additionalProperties":false,"properties":` +
				`{"text":{"type":"string"}},"required":["text"]}`,
		),
		Name: "echo", Label: "Echo", Description: "Echo supplied text", PromptSnippet: "Return supplied text",
		PromptGuidelines: []string{"Pass text."}, ReadOnly: true,
	}
}

func (executor *executeTestTool) Execute(_ context.Context, input tool.Arguments) (tool.Result, error) {
	var args struct {
		Text string `json:"text"`
	}
	if err := input.Decode(&args); err != nil {
		return tool.Result{}, fmt.Errorf("decode echo input: %w", err)
	}

	return tool.TextResult(args.Text, map[string]any{"length": len(args.Text)}), nil
}

func TestExecuteToolUsesCompletedPromptRegistry(t *testing.T) {
	t.Parallel()

	registry, err := tool.NewRegistryWithTools(t.TempDir(), nil)
	require.NoError(t, err)

	echo := new(executeTestTool)
	require.NoError(t, registry.Register(echo))
	execute := newExecuteTool(nil, registry)
	require.NoError(t, registry.Register(execute))

	tests := []struct {
		check  func(*testing.T, tool.Result)
		name   string
		source string
	}{
		{
			name:   "search",
			source: `import "tools"; tools.Search("echo")`,
			check: func(t *testing.T, result tool.Result) {
				t.Helper()
				assert.Contains(t, result.Text(), `"name":"echo"`)
				assert.NotContains(t, result.Text(), `"name":"execute"`)
			},
		},
		{
			name:   "describe includes schema",
			source: `import "tools"; tools.Describe("echo")`,
			check: func(t *testing.T, result tool.Result) {
				t.Helper()
				assert.Contains(t, result.Text(), `"required":["text"]`)
			},
		},
		{
			name: "multiple calls",
			source: `import "tools"
first := tools.Call("echo", map[string]interface{}{"text": "one"})
second := tools.Call("echo", map[string]interface{}{"text": "two"})
[]interface{}{first, second}`,
			check: func(t *testing.T, result tool.Result) {
				t.Helper()
				assert.Contains(t, result.Text(), `"text":"one"`)
				assert.Contains(t, result.Text(), `"length":3`)
				assert.NotNil(t, result.Details[executeResultValueKey])
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result, executeErr := execute.Execute(t.Context(), executeArguments(t, test.source))
			require.NoError(t, executeErr)
			test.check(t, result)
		})
	}
}

func TestExecuteToolRejectsRecursionAndInvalidNestedInput(t *testing.T) {
	t.Parallel()

	registry, err := tool.NewRegistryWithTools(t.TempDir(), nil)
	require.NoError(t, err)

	execute := newExecuteTool(nil, registry)
	require.NoError(t, registry.Register(execute))

	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "call", source: `import "tools"; tools.Call("execute", map[string]interface{}{})`,
			want: "execute cannot call itself",
		},
		{name: "describe", source: `import "tools"; tools.Describe("execute")`, want: "null"},
		{
			name: "unknown", source: `import "tools"; tools.Call("missing", map[string]interface{}{})`,
			want: "unknown tool",
		},
		{name: "non object input", source: `import "tools"; tools.Call("missing", "bad")`, want: "cannot unmarshal"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result, executeErr := execute.Execute(t.Context(), executeArguments(t, test.source))
			require.NoError(t, executeErr)
			assert.Contains(t, result.Text(), test.want)
		})
	}
}

func TestExecuteNestedCallsUseSharedInvocationBoundary(t *testing.T) {
	t.Parallel()

	runtime := newToolExecutorTestRuntime(nil)
	registry, err := tool.NewRegistryWithTools(t.TempDir(), nil)
	require.NoError(t, err)
	require.NoError(t, registry.Register(new(executeTestTool)))
	execute := newExecuteTool(runtime, registry)
	require.NoError(t, registry.Register(execute))

	events := []StreamEvent{}
	outer := runtime.executeProviderToolCall(
		t.Context(), registry,
		&ToolCall{
			Metadata: nil, Arguments: executeArguments(t, `import "tools"
 tools.Call("echo", map[string]interface{}{"text": "one"})
 tools.Call("echo", map[string]interface{}{"text": "two"})`),
			ID: executeOuterCallID, Name: string(executeToolName), ArgumentsJSON: `{}`,
		},
		func(event StreamEvent) { events = append(events, event) },
	)

	require.False(t, outer.IsError)
	require.Len(t, events, 6)
	assert.Equal(t, StreamEventToolStart, events[0].Kind)
	assert.Equal(t, executeOuterCallID, events[0].ToolCallEvent.ID)
	assert.Equal(t, "echo", events[1].ToolCallEvent.Name)
	assert.Equal(t, "outer/1", events[1].ToolCallEvent.ID)
	assert.Equal(t, executeOuterCallID, events[1].ToolCallEvent.ParentCallID)
	assert.Equal(t, 1, events[1].ToolCallEvent.Sequence)
	assert.Equal(t, "one", events[2].ToolEvent.Result)
	assert.Equal(t, events[1].ToolCallEvent.ID, events[2].ToolEvent.CallID)
	assert.Equal(t, executeOuterCallID, events[2].ToolEvent.ParentCallID)
	assert.Equal(t, 1, events[2].ToolEvent.Sequence)
	assert.Equal(t, "echo", events[3].ToolCallEvent.Name)
	assert.Equal(t, "outer/2", events[3].ToolCallEvent.ID)
	assert.Equal(t, executeOuterCallID, events[3].ToolCallEvent.ParentCallID)
	assert.Equal(t, 2, events[3].ToolCallEvent.Sequence)
	assert.Equal(t, "two", events[4].ToolEvent.Result)
	assert.Equal(t, events[3].ToolCallEvent.ID, events[4].ToolEvent.CallID)
	assert.Equal(t, executeOuterCallID, events[4].ToolEvent.ParentCallID)
	assert.Equal(t, 2, events[4].ToolEvent.Sequence)
	assert.Equal(t, StreamEventToolResult, events[5].Kind)
	assert.Equal(t, executeOuterCallID, events[5].ToolEvent.CallID)
}

func TestPromptRegistryRegistersExecuteAfterPromptLocalTools(t *testing.T) {
	t.Parallel()

	runtime := new(Runtime)
	runtime.profile = topLevelExecutionProfile()
	registry, err := runtime.promptToolRegistry(t.TempDir(), "owner")
	require.NoError(t, err)

	definitions := registry.Definitions()
	found := false

	for _, definition := range definitions {
		if definition.Name == executeToolName {
			found = true

			assert.False(t, definition.ReadOnly)
		}
	}

	assert.True(t, found)
}

func executeArguments(t *testing.T, source string) tool.Arguments {
	t.Helper()

	encoded, err := json.Marshal(map[string]string{"source": source})
	require.NoError(t, err)
	arguments, err := tool.ArgumentsFromRaw(encoded)
	require.NoError(t, err)

	return arguments
}

var _ tool.Executor = (*executeTestTool)(nil)
