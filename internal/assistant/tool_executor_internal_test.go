package assistant

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/testutil"
	"github.com/omarluq/librecode/internal/tool"
)

const (
	toolExecutorCallID      = "call_1"
	toolExecutorMissingTool = "missing"
	toolExecutorReadPath    = "README.md"
	toolExecutorReadArgs    = `{"path":"README.md"}`
	toolExecutorFirstName   = "one"
	toolExecutorThirdName   = "three"
	sequentialTestToolName  = "sequential"
	fastTestToolName        = "fast"
	slowTestToolName        = "slow"
)

func TestExecuteProviderToolCallsRequiresRegistry(t *testing.T) {
	t.Parallel()

	runtime := newToolExecutorTestRuntime(nil)
	executor := runtime.executeProviderToolCalls(nil, "", "")

	events, err := executor(context.Background(), []ToolCall{{
		Metadata:      nil,
		Arguments:     tool.EmptyArguments(),
		ID:            "",
		Name:          jsonReadToolName,
		ArgumentsJSON: "",
	}}, nil)

	require.Error(t, err)
	assert.Nil(t, events)

	var coded oops.OopsError
	require.ErrorAs(t, err, &coded)
	assert.Equal(t, "tool_registry_missing", coded.Code())
}

func TestExecuteProviderToolCallsRunsAllCalls(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	writeToolExecutorReadFixture(t, directory)

	runtime := newToolExecutorTestRuntime(nil)
	executor := runtime.executeProviderToolCalls(tool.NewRegistry(directory), "", "")

	events, err := executor(context.Background(), []ToolCall{
		{
			Metadata:      nil,
			Arguments:     testutil.ToolArguments(map[string]any{jsonPathKey: toolExecutorReadPath}),
			ID:            toolExecutorCallID,
			Name:          jsonReadToolName,
			ArgumentsJSON: toolExecutorReadArgs,
		},
		{
			Metadata:      nil,
			Arguments:     tool.EmptyArguments(),
			ID:            "call_2",
			Name:          toolExecutorMissingTool,
			ArgumentsJSON: `{}`,
		},
	}, nil)

	require.NoError(t, err)
	require.Len(t, events, 2)
	assert.False(t, events[0].IsError)
	assert.True(t, events[1].IsError)
}

func TestExecuteProviderToolCallsRunsWholeBatchSequentiallyWhenRequired(t *testing.T) {
	t.Parallel()

	started := make(chan tool.Name, 3)
	release := make(chan struct{}, 3)

	toolRegistry, err := tool.NewRegistryWithTools(t.TempDir(), []tool.Name{})
	require.NoError(t, err)

	for _, definition := range []struct {
		name       tool.Name
		sequential bool
	}{
		{name: toolExecutorFirstName, sequential: false},
		{name: sequentialTestToolName, sequential: true},
		{name: toolExecutorThirdName, sequential: false},
	} {
		require.NoError(t, toolRegistry.Register(&blockingToolExecutor{
			started: started, release: release, name: definition.name, sequential: definition.sequential,
		}))
	}

	calls := []ToolCall{
		{
			Metadata: nil, ID: toolExecutorFirstName, Name: toolExecutorFirstName,
			Arguments: tool.EmptyArguments(), ArgumentsJSON: `{}`,
		},
		{
			Metadata: nil, ID: sequentialTestToolName, Name: sequentialTestToolName,
			Arguments: tool.EmptyArguments(), ArgumentsJSON: `{}`,
		},
		{
			Metadata: nil, ID: toolExecutorThirdName, Name: toolExecutorThirdName,
			Arguments: tool.EmptyArguments(), ArgumentsJSON: `{}`,
		},
	}

	type result struct {
		err    error
		events []ToolEvent
	}

	done := make(chan result, 1)

	go func() {
		events, executeErr := newToolExecutorTestRuntime(nil).executeProviderToolCalls(toolRegistry, "", "")(
			t.Context(), calls, nil,
		)
		done <- result{events: events, err: executeErr}
	}()

	assert.Equal(t, tool.Name(toolExecutorFirstName), <-started)

	select {
	case name := <-started:
		t.Fatalf("tool %q overlapped a sequential batch", name)
	default:
	}

	release <- struct{}{}

	assert.Equal(t, tool.Name(sequentialTestToolName), <-started)

	release <- struct{}{}

	assert.Equal(t, tool.Name(toolExecutorThirdName), <-started)

	release <- struct{}{}

	execution := <-done
	require.NoError(t, execution.err)
	require.Len(t, execution.events, len(calls))

	for index := range calls {
		assert.Equal(t, calls[index].ID, execution.events[index].CallID)
	}
}

func TestExecuteProviderToolCallsBoundsParallelism(t *testing.T) {
	t.Parallel()

	const (
		callCount        = 16
		concurrencyLimit = 4
	)

	started := make(chan tool.Name, callCount)
	release := make(chan struct{})
	toolRegistry, err := tool.NewRegistryWithTools(t.TempDir(), []tool.Name{})
	require.NoError(t, err)

	calls := make([]ToolCall, callCount)
	for index := range callCount {
		name := tool.Name(fmt.Sprintf("parallel_%d", index))
		require.NoError(t, toolRegistry.Register(&blockingToolExecutor{
			started: started, release: release, name: name, sequential: false,
		}))
		calls[index] = ToolCall{
			Metadata: nil, ID: string(name), Name: string(name), Arguments: tool.EmptyArguments(), ArgumentsJSON: `{}`,
		}
	}

	done := make(chan error, 1)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	go func() {
		_, executeErr := newToolExecutorTestRuntime(nil).executeProviderToolCalls(toolRegistry, "", "")(ctx, calls, nil)
		done <- executeErr
	}()

	for range concurrencyLimit {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatalf("initial tool calls did not start: %v", ctx.Err())
		}
	}

	select {
	case name := <-started:
		t.Fatalf("tool call %q exceeded concurrency limit", name)
	case <-time.After(50 * time.Millisecond):
	}

	close(release)

	select {
	case executeErr := <-done:
		require.NoError(t, executeErr)
	case <-ctx.Done():
		t.Fatalf("tool calls did not finish: %v", ctx.Err())
	}
}

func TestExecuteProviderToolCallsEmitsResultsAsCallsComplete(t *testing.T) {
	t.Parallel()

	started := make(chan tool.Name, 2)
	slowRelease := make(chan struct{})
	fastRelease := make(chan struct{})
	registry, err := tool.NewRegistryWithTools(t.TempDir(), nil)
	require.NoError(t, err)
	require.NoError(t, registry.Register(&blockingToolExecutor{
		started: started, release: slowRelease, name: slowTestToolName, sequential: false,
	}))
	require.NoError(t, registry.Register(&blockingToolExecutor{
		started: started, release: fastRelease, name: fastTestToolName, sequential: false,
	}))

	events := make(chan StreamEvent, 16)

	type executionResult struct {
		err    error
		events []ToolEvent
	}

	done := make(chan executionResult, 1)

	go func() {
		results, executeErr := newToolExecutorTestRuntime(nil).executeProviderToolCalls(registry, "", "")(
			t.Context(),
			[]ToolCall{
				{
					Metadata: nil, ID: slowTestToolName, Name: slowTestToolName,
					Arguments: tool.EmptyArguments(), ArgumentsJSON: `{}`,
				},
				{
					Metadata: nil, ID: fastTestToolName, Name: fastTestToolName,
					Arguments: tool.EmptyArguments(), ArgumentsJSON: `{}`,
				},
			},
			func(event StreamEvent) { events <- event },
		)
		done <- executionResult{events: results, err: executeErr}
	}()

	for range 2 {
		<-started
	}

	close(fastRelease)

	var completed *ToolEvent
	for completed == nil {
		event := <-events
		if event.Kind == StreamEventToolResult {
			completed = event.ToolEvent
		}
	}

	require.NotNil(t, completed)
	assert.Equal(t, fastTestToolName, completed.CallID)

	select {
	case <-done:
		t.Fatal("batch completed before slow call was released")
	default:
	}

	close(slowRelease)

	execution := <-done
	require.NoError(t, execution.err)
	require.Len(t, execution.events, 2)
	assert.Equal(t, slowTestToolName, execution.events[0].CallID)
	assert.Equal(t, fastTestToolName, execution.events[1].CallID)
}

func TestExecuteProviderToolCallEmitsResultForUnknownTool(t *testing.T) {
	t.Parallel()

	runtime := newToolExecutorTestRuntime(nil)
	registry := tool.NewRegistry(t.TempDir())
	streamEvents := []StreamEvent{}

	event := runtime.executeProviderToolCall(
		context.Background(),
		registry,
		&ToolCall{
			Metadata:      nil,
			Arguments:     tool.EmptyArguments(),
			ID:            toolExecutorCallID,
			Name:          toolExecutorMissingTool,
			ArgumentsJSON: `{}`,
		},
		func(event StreamEvent) { streamEvents = append(streamEvents, event) },
	)

	assert.True(t, event.IsError)
	assert.Contains(t, event.Error, "unknown tool")
	require.Len(t, streamEvents, 2)
	assert.Equal(t, StreamEventToolStart, streamEvents[0].Kind)
	assert.Equal(t, "missing", streamEvents[0].Text)
	assert.Equal(t, StreamEventToolResult, streamEvents[1].Kind)
	require.NotNil(t, streamEvents[1].ToolEvent)
	assert.True(t, streamEvents[1].ToolEvent.IsError)
}

func TestExecuteProviderToolCallReturnsLifecycleErrorEvent(t *testing.T) {
	t.Parallel()

	runtime := newToolExecutorTestRuntime(failingToolCallLifecycle{})
	streamEvents := []StreamEvent{}

	event := runtime.executeProviderToolCall(
		context.Background(),
		tool.NewRegistry(t.TempDir()),
		&ToolCall{
			Metadata:      nil,
			Arguments:     testutil.ToolArguments(map[string]any{jsonPathKey: toolExecutorReadPath}),
			ID:            toolExecutorCallID,
			Name:          jsonReadToolName,
			ArgumentsJSON: toolExecutorReadArgs,
		},
		func(event StreamEvent) { streamEvents = append(streamEvents, event) },
	)

	assert.True(t, event.IsError)
	assert.Contains(t, event.Error, "blocked")
	require.Len(t, streamEvents, 1)
	assert.Equal(t, StreamEventToolResult, streamEvents[0].Kind)
}

func TestExecuteProviderToolCallPreservesResultOnLifecycleError(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	writeToolExecutorReadFixture(t, directory)

	runtime := newToolExecutorTestRuntime(failingToolResultLifecycle{})
	registry := tool.NewRegistry(directory)
	events := []StreamEvent{}

	event := runtime.executeProviderToolCall(
		context.Background(),
		registry,
		&ToolCall{
			Metadata:      nil,
			Arguments:     testutil.ToolArguments(map[string]any{jsonPathKey: toolExecutorReadPath}),
			ID:            toolExecutorCallID,
			Name:          jsonReadToolName,
			ArgumentsJSON: toolExecutorReadArgs,
		},
		func(event StreamEvent) { events = append(events, event) },
	)

	assert.False(t, event.IsError)
	assert.Empty(t, event.Error)
	assert.NotContains(t, event.Result, "result hook failed")
	require.Len(t, events, 2)
	require.NotNil(t, events[1].ToolEvent)
	assert.Equal(t, event.Result, events[1].ToolEvent.Result)
}

func TestCanonicalToolResultUsesLifecycleMutation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		wantDetails map[string]any
		name        string
		detailsJSON string
	}{
		{name: "valid details", detailsJSON: `{"redacted":true}`, wantDetails: map[string]any{"redacted": true}},
		{name: "malformed details fail closed", detailsJSON: `{"redacted":`, wantDetails: nil},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := tool.TextResult("original", map[string]any{"old": true})
			event := ToolEvent{
				CallID: executeCallMethod, ParentCallID: "", Name: jsonReadToolName, ArgumentsJSON: "{}",
				DetailsJSON: testCase.detailsJSON, Result: "redacted", Error: "", Sequence: 0, IsError: false,
			}

			canonical := canonicalToolResult(result, &event)
			assert.Equal(t, "redacted", canonical.Text())
			assert.Equal(t, testCase.wantDetails, canonical.Details)
		})
	}
}

func TestEncodeToolDetails(t *testing.T) {
	t.Parallel()

	assert.Empty(t, encodeToolDetails(nil))
	assert.JSONEq(t, `{"count":1}`, encodeToolDetails(map[string]any{"count": 1}))
	assert.Empty(t, encodeToolDetails(map[string]any{"bad": func() {}}))
}

func TestToolEventFromResultFormatsEmptyOutput(t *testing.T) {
	t.Parallel()

	event := toolEventFromResult(
		&ToolCallEvent{
			ParentCallID: "",
			Sequence:     0,

			Arguments:     tool.EmptyArguments(),
			ID:            "",
			Name:          jsonReadToolName,
			ArgumentsJSON: `{}`,
		},
		tool.TextResult("   ", nil),
		nil,
	)

	assert.False(t, event.IsError)
	assert.Equal(t, "(tool returned no text output)", event.Result)
}

func TestLLMToolResultFromToolEvent(t *testing.T) {
	t.Parallel()

	empty := llmToolResultFromToolEvent(nil)
	assert.Empty(t, empty.Name)
	assert.False(t, empty.IsError)

	result := llmToolResultFromToolEvent(&ToolEvent{
		CallID:       "",
		ParentCallID: "",
		Sequence:     0,

		Name:          jsonReadToolName,
		ArgumentsJSON: toolExecutorReadArgs,
		DetailsJSON:   `{"diff":"+added"}`,
		Result:        "contents",
		Error:         "boom",
		IsError:       true,
	})

	assert.Equal(t, expectedReadToolName, result.Name)
	assert.JSONEq(t, toolExecutorReadArgs, result.ArgumentsJSON)
	detailsJSON, ok := result.Metadata["details_json"].(string)
	require.True(t, ok)
	assert.JSONEq(t, `{"diff":"+added"}`, detailsJSON)
	assert.Equal(t, "boom", result.Error)
	assert.True(t, result.IsError)
	require.Len(t, result.Content, 1)
	assert.Equal(t, llm.PartText, result.Content[0].Type)
	assert.Equal(t, "contents", result.Content[0].Text)
}

type blockingToolExecutor struct {
	started    chan<- tool.Name
	release    <-chan struct{}
	name       tool.Name
	sequential bool
}

func (executor *blockingToolExecutor) Sequential() bool {
	return executor.sequential
}

func (executor *blockingToolExecutor) Definition() tool.Definition {
	return tool.Definition{
		Schema: tool.EmptySchema(), Name: executor.name, Label: string(executor.name), Description: "test tool",
		PromptSnippet: "", PromptGuidelines: []string{}, ReadOnly: true,
	}
}

func (executor *blockingToolExecutor) Execute(ctx context.Context, _ tool.Arguments) (tool.Result, error) {
	if executor.started != nil {
		executor.started <- executor.name
	}

	if executor.release != nil {
		select {
		case <-executor.release:
		case <-ctx.Done():
			return tool.Result{Content: nil, Details: nil}, fmt.Errorf("blocked test tool: %w", ctx.Err())
		}
	}

	return tool.TextResult(string(executor.name), nil), nil
}

func writeToolExecutorReadFixture(t *testing.T, directory string) {
	t.Helper()

	require.NoError(t, os.WriteFile(filepath.Join(directory, toolExecutorReadPath), []byte("contents"), 0o600))
}

func newToolExecutorTestRuntime(extensions runtimeExtensions) *Runtime {
	return newRuntimeFromDeps(func(deps *runtimeDeps) {
		deps.Extensions = extensions
	})
}

type failingToolCallLifecycle struct{}

type failingToolResultLifecycle struct{}

func (failingToolCallLifecycle) ExecuteCommand(context.Context, string, string) (string, error) {
	return "", nil
}

func (failingToolResultLifecycle) ExecuteCommand(context.Context, string, string) (string, error) {
	return "", nil
}

func (failingToolCallLifecycle) Emit(context.Context, string, map[string]any) error {
	return nil
}

func (failingToolResultLifecycle) Emit(context.Context, string, map[string]any) error {
	return nil
}

func (failingToolCallLifecycle) ExecuteTool(context.Context, string, tool.Arguments) (extension.ToolResult, error) {
	return extension.ToolResult{Details: nil, Content: ""}, nil
}

func (failingToolResultLifecycle) ExecuteTool(context.Context, string, tool.Arguments) (extension.ToolResult, error) {
	return extension.ToolResult{Details: nil, Content: ""}, nil
}

func (failingToolCallLifecycle) Tools() []extension.Tool {
	return nil
}

func (failingToolResultLifecycle) Tools() []extension.Tool {
	return nil
}

func (failingToolCallLifecycle) DispatchLifecycle(
	_ context.Context,
	event extension.LifecycleEvent,
) (extension.LifecycleDispatchResult, error) {
	if event.Name == extension.LifecycleToolCall {
		return emptyTestLifecycleDispatchResult(event), errors.New("blocked")
	}

	return emptyTestLifecycleDispatchResult(event), nil
}

func (failingToolResultLifecycle) DispatchLifecycle(
	_ context.Context,
	event extension.LifecycleEvent,
) (extension.LifecycleDispatchResult, error) {
	if event.Name == extension.LifecycleToolResult {
		return emptyTestLifecycleDispatchResult(event), errors.New("result hook failed")
	}

	return emptyTestLifecycleDispatchResult(event), nil
}

func emptyTestLifecycleDispatchResult(event extension.LifecycleEvent) extension.LifecycleDispatchResult {
	return extension.LifecycleDispatchResult{
		Payload:         event.Payload,
		ProviderRequest: extension.ProviderRequestMutation{Headers: nil},
		ToolCall:        extension.ToolCallMutation{Arguments: tool.EmptyArguments(), HasArgs: false},
		ToolResult:      extension.ToolResultMutation{Result: nil, DetailsJSON: nil, Error: nil},
		Compaction: extension.CompactionMutation{
			Summary:          nil,
			FirstKeptEntryID: nil,
			Details:          nil,
			Cancel:           false,
		},
		Name:         string(event.Name),
		Errors:       []string{},
		Duration:     0,
		HandlerCount: 0,
		Consumed:     false,
		Stopped:      false,
	}
}
