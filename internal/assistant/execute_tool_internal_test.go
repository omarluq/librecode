package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/executeworker"
	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/mvmhost"
	"github.com/omarluq/librecode/internal/tool"
)

const (
	executeOuterCallID  = "outer"
	executeTestToolName = "echo"
)

type executeTestTool struct{}

func (executor *executeTestTool) Definition() tool.Definition {
	return tool.Definition{
		Schema: mustToolSchema(
			`{"type":"object","additionalProperties":false,"properties":` +
				`{"text":{"type":"string"}},"required":["text"]}`,
		),
		Name: executeTestToolName, Label: "Echo", Description: "Echo supplied text",
		PromptSnippet:    "Return supplied text",
		PromptGuidelines: []string{"Pass text."}, ReadOnly: true,
	}
}

func (executor *executeTestTool) Execute(_ context.Context, input tool.Arguments) (tool.Result, error) {
	var args struct {
		Text string `json:"text"`
	}
	if err := input.Decode(&args); err != nil {
		return tool.Result{}, oops.In("assistant").Code("execute_test_echo_input").
			Wrapf(err, "decode echo input")
	}

	return tool.TextResult(args.Text, map[string]any{"length": len(args.Text)}), nil
}

func TestExecuteToolUsesCompletedPromptRegistry(t *testing.T) {
	t.Parallel()

	registry, err := tool.NewRegistryWithTools(t.TempDir(), nil)
	require.NoError(t, err)

	echo := new(executeTestTool)
	require.NoError(t, registry.Register(echo))
	execute := newExecuteTool(registry, nil)
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

	execute := newExecuteTool(registry, nil)
	require.NoError(t, registry.Register(execute))

	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: executeCallMethod, source: `import "tools"; tools.Call("execute", map[string]interface{}{})`,
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

	invoke := func(
		ctx context.Context,
		name string,
		arguments tool.Arguments,
		argumentsJSON string,
	) (tool.Result, ToolEvent) {
		return runtime.invokeNestedTool(ctx, registry, name, arguments, argumentsJSON)
	}
	execute := newExecuteTool(registry, invoke)
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

func TestExecuteToolHardCancelsHungProgram(t *testing.T) {
	t.Parallel()

	registry, err := tool.NewRegistryWithTools(t.TempDir(), nil)
	require.NoError(t, err)

	execute := newExecuteTool(registry, nil)

	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()

	started := time.Now()
	_, err = execute.Execute(ctx, executeArguments(t, `for {}; 1`))
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(started), 3*time.Second)
}

func TestExecuteNestedCallsPreserveMixedResultsAndLifecycleOrder(t *testing.T) {
	t.Parallel()

	recorder := &executeLifecycleRecorder{runtimeExtensions: nil, events: nil, mu: sync.Mutex{}}
	runtime := newToolExecutorTestRuntime(recorder)
	registry, err := tool.NewRegistryWithTools(t.TempDir(), nil)
	require.NoError(t, err)
	require.NoError(t, registry.Register(new(executeTestTool)))
	execute := newExecuteTool(registry, func(
		ctx context.Context,
		name string,
		arguments tool.Arguments,
		argumentsJSON string,
	) (tool.Result, ToolEvent) {
		return runtime.invokeNestedTool(ctx, registry, name, arguments, argumentsJSON)
	})
	require.NoError(t, registry.Register(execute))

	events := []StreamEvent{}
	outer := runtime.executeProviderToolCall(t.Context(), registry, &ToolCall{
		Metadata: nil,
		Arguments: executeArguments(t, `import "tools"
[]interface{}{
	tools.Call("echo", map[string]interface{}{"text": "ok"}),
	tools.Call("missing", map[string]interface{}{}),
}`),
		ID: executeOuterCallID, Name: string(executeToolName), ArgumentsJSON: `{}`,
	}, func(event StreamEvent) { events = append(events, event) })

	require.False(t, outer.IsError)
	require.Len(t, events, 6)
	assert.Equal(t, "ok", events[2].ToolEvent.Result)
	assert.False(t, events[2].ToolEvent.IsError)
	assert.Contains(t, events[4].ToolEvent.Error, "unknown tool")
	assert.True(t, events[4].ToolEvent.IsError)
	assert.Equal(t, []string{
		string(extension.LifecycleToolCall),
		string(extension.LifecycleToolCall),
		string(extension.LifecycleToolResult),
		string(extension.LifecycleToolCall),
		string(extension.LifecycleToolResult),
		string(extension.LifecycleToolError),
		string(extension.LifecycleToolResult),
	}, recorder.eventNames())
}

func TestExecuteNestedCallCancellationEmitsCompletedBoundaries(t *testing.T) {
	t.Parallel()

	runtime := newToolExecutorTestRuntime(nil)
	registry, err := tool.NewRegistryWithTools(t.TempDir(), nil)
	require.NoError(t, err)

	blocking := &executeBlockingTool{started: make(chan struct{})}
	require.NoError(t, registry.Register(blocking))
	execute := newExecuteTool(registry, func(
		ctx context.Context,
		name string,
		arguments tool.Arguments,
		argumentsJSON string,
	) (tool.Result, ToolEvent) {
		return runtime.invokeNestedTool(ctx, registry, name, arguments, argumentsJSON)
	})
	require.NoError(t, registry.Register(execute))

	ctx, cancel := context.WithCancel(t.Context())
	events := []StreamEvent{}

	done := make(chan ToolEvent, 1)
	arguments := executeArguments(t, `import "tools"; tools.Call("block", map[string]interface{}{})`)

	go func() {
		done <- runtime.executeProviderToolCall(ctx, registry, &ToolCall{
			Metadata:  nil,
			Arguments: arguments,
			ID:        executeOuterCallID, Name: string(executeToolName), ArgumentsJSON: `{}`,
		}, func(event StreamEvent) { events = append(events, event) })
	}()

	<-blocking.started
	cancel()

	outer := <-done

	assert.True(t, outer.IsError)
	require.Len(t, events, 4)
	assert.Equal(t, []StreamEventKind{
		StreamEventToolStart, StreamEventToolStart, StreamEventToolResult, StreamEventToolResult,
	}, []StreamEventKind{events[0].Kind, events[1].Kind, events[2].Kind, events[3].Kind})
	assert.Equal(t, executeOuterCallID, events[2].ToolEvent.ParentCallID)
	assert.True(t, events[2].ToolEvent.IsError)
}

func TestExecuteResultTextRejectsOversizedResult(t *testing.T) {
	t.Parallel()

	_, err := executeResultText(mvmhost.Result{
		Value: strings.Repeat("x", defaultExecuteResultLimit), ValueKind: "", Stdout: "", Stderr: "",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "execute result")
	assert.Contains(t, err.Error(), "limit")
}

func TestExecuteToolValidationAndWorkerErrors(t *testing.T) {
	t.Parallel()

	missingRegistry := newExecuteTool(nil, nil)
	_, err := missingRegistry.Execute(t.Context(), tool.EmptyArguments())
	require.ErrorContains(t, err, "registry is not configured")

	registry, registryErr := tool.NewRegistryWithTools(t.TempDir(), nil)
	require.NoError(t, registryErr)

	execute := newExecuteTool(registry, nil)

	invalidInput, inputErr := tool.ArgumentsFromRaw([]byte(`{"source":42}`))
	require.NoError(t, inputErr)
	_, err = execute.Execute(t.Context(), invalidInput)
	require.ErrorContains(t, err, "decode execute input")

	_, err = execute.handleWorkerMessage(t.Context(), executeTestWorkerMessage("unknown", "", nil))
	require.ErrorContains(t, err, "unknown execute worker RPC method")

	_, err = execute.handleWorkerMessage(
		t.Context(),
		executeTestWorkerMessage(executeCallMethod, executeTestToolName, json.RawMessage(`[]`)),
	)
	require.ErrorContains(t, err, "decode nested tool input")
}

func executeTestWorkerMessage(method, name string, input json.RawMessage) *executeworker.Message {
	return &executeworker.Message{
		Stderr: "", Source: "", Method: method, Mode: "", Name: name, Query: "", Stdout: "", Type: "",
		Error: "", ErrorKind: "", ValueKind: "", Input: input, Value: nil, Arguments: nil, ID: 0,
		ExitCode: 0,
	}
}

func TestExecuteCallHandlesProtectedToolsAndInvocationError(t *testing.T) {
	t.Parallel()

	registry, err := tool.NewRegistryWithTools(t.TempDir(), nil)
	require.NoError(t, err)

	execute := newExecuteTool(registry, func(
		context.Context,
		string,
		tool.Arguments,
		string,
	) (tool.Result, ToolEvent) {
		return tool.Result{Details: nil, Content: nil}, ToolEvent{
			CallID: "", ParentCallID: "", Name: executeTestToolName, ArgumentsJSON: "", DetailsJSON: "",
			Result: "", Error: "nested failure", Sequence: 0, IsError: true,
		}
	})

	workflowResult := execute.call(t.Context(), string(workflowToolName), json.RawMessage(`{}`))
	assert.True(t, workflowResult.IsError)
	assert.Equal(t, "execute cannot call workflow", workflowResult.Error)

	invalidResult := execute.call(t.Context(), executeTestToolName, json.RawMessage(`[]`))
	assert.True(t, invalidResult.IsError)
	assert.Contains(t, invalidResult.Error, "decode tool arguments")

	invocationResult := execute.call(t.Context(), executeTestToolName, json.RawMessage(`{}`))
	assert.True(t, invocationResult.IsError)
	assert.Equal(t, "nested failure", invocationResult.Error)
}

func TestExecuteResultTextVariants(t *testing.T) {
	t.Parallel()

	text, err := executeResultText(mvmhost.Result{
		Value: nil, ValueKind: "", Stdout: "printed", Stderr: "",
	})
	require.NoError(t, err)
	assert.Equal(t, "printed", text)

	text, err = executeResultText(mvmhost.Result{Value: nil, ValueKind: "", Stdout: "", Stderr: ""})
	require.NoError(t, err)
	assert.Equal(t, "null", text)

	text, err = executeResultText(mvmhost.Result{
		Value: map[string]any{"ok": true}, ValueKind: "", Stdout: "", Stderr: "",
	})
	require.NoError(t, err)
	assert.JSONEq(t, `{"ok":true}`, text)

	_, err = executeResultText(mvmhost.Result{
		Value: make(chan struct{}), ValueKind: "", Stdout: "", Stderr: "",
	})
	require.ErrorContains(t, err, "encode execute result")

	definition := tool.Definition{
		Schema: tool.EmptySchema(), Name: "empty", Label: "", Description: "", PromptSnippet: "",
		PromptGuidelines: nil, ReadOnly: false,
	}
	assert.Nil(t, executeDefinitionMap(&definition)["schema"])
}

type executeBlockingTool struct {
	started chan struct{}
}

func (executor *executeBlockingTool) Definition() tool.Definition {
	return tool.Definition{
		Schema: mustToolSchema(`{"type":"object","additionalProperties":false}`),
		Name:   "block", Label: "Block", Description: "Block until canceled", PromptSnippet: "Block",
		PromptGuidelines: nil, ReadOnly: true,
	}
}

func (executor *executeBlockingTool) Execute(ctx context.Context, _ tool.Arguments) (tool.Result, error) {
	close(executor.started)
	<-ctx.Done()

	return tool.Result{}, oops.In("assistant").Code("blocking_tool_canceled").Wrapf(ctx.Err(), "wait for cancellation")
}

type executeLifecycleRecorder struct {
	runtimeExtensions
	events []string
	mu     sync.Mutex
}

func (recorder *executeLifecycleRecorder) DispatchLifecycle(
	_ context.Context,
	event extension.LifecycleEvent,
) (extension.LifecycleDispatchResult, error) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()

	recorder.events = append(recorder.events, string(event.Name))

	return emptyTestLifecycleDispatchResult(event), nil
}

func (recorder *executeLifecycleRecorder) eventNames() []string {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()

	return append([]string(nil), recorder.events...)
}

func TestPromptRegistryRegistersExecuteAfterPromptLocalTools(t *testing.T) {
	t.Parallel()

	runtime := new(Runtime)
	runtime.profile = topLevelExecutionProfile()
	registry, err := runtime.promptToolRegistry(t.Context(), t.TempDir(), "owner")
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
