package assistant

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/tool"
)

const (
	toolExecutorCallID   = "call_1"
	toolExecutorReadPath = "README.md"
	toolExecutorReadArgs = `{"path":"README.md"}`
)

func TestExecuteProviderToolCallsRequiresRegistry(t *testing.T) {
	t.Parallel()

	runtime := newToolExecutorTestRuntime(nil)
	executor := runtime.executeProviderToolCalls(nil)

	events, err := executor(context.Background(), []ToolCall{{
		Metadata:      nil,
		Arguments:     nil,
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

func TestExecuteProviderToolCallEmitsResultForUnknownTool(t *testing.T) {
	t.Parallel()

	runtime := newToolExecutorTestRuntime(nil)
	registry := tool.NewRegistry(t.TempDir())
	streamEvents := []StreamEvent{}

	event := runtime.executeProviderToolCall(
		context.Background(),
		registry,
		ToolCall{
			Metadata:      nil,
			Arguments:     map[string]any{},
			ID:            toolExecutorCallID,
			Name:          "missing",
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
		ToolCall{
			Metadata:      nil,
			Arguments:     map[string]any{jsonPathKey: toolExecutorReadPath},
			ID:            toolExecutorCallID,
			Name:          jsonReadToolName,
			ArgumentsJSON: toolExecutorReadArgs,
		},
		func(event StreamEvent) { streamEvents = append(streamEvents, event) },
	)

	assert.True(t, event.IsError)
	assert.Contains(t, event.Error, "blocked")
	require.Len(t, streamEvents, 2)
	assert.Equal(t, StreamEventToolResult, streamEvents[1].Kind)
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
		ToolCall{
			Metadata:      nil,
			Arguments:     map[string]any{jsonPathKey: toolExecutorReadPath},
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

func TestToolEventFromResultFormatsEmptyOutput(t *testing.T) {
	t.Parallel()

	event := toolEventFromResult(
		ToolCallEvent{
			Arguments:     nil,
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
		Name:          jsonReadToolName,
		ArgumentsJSON: toolExecutorReadArgs,
		DetailsJSON:   "",
		Result:        "contents",
		Error:         "boom",
		IsError:       true,
	})

	assert.Equal(t, expectedReadToolName, result.Name)
	assert.JSONEq(t, toolExecutorReadArgs, result.ArgumentsJSON)
	assert.Equal(t, "boom", result.Error)
	assert.True(t, result.IsError)
	require.Len(t, result.Content, 1)
	assert.Equal(t, llm.PartText, result.Content[0].Type)
	assert.Equal(t, "contents", result.Content[0].Text)
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

func (failingToolCallLifecycle) ExecuteTool(context.Context, string, map[string]any) (extension.ToolResult, error) {
	return extension.ToolResult{Details: nil, Content: ""}, nil
}

func (failingToolResultLifecycle) ExecuteTool(context.Context, string, map[string]any) (extension.ToolResult, error) {
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
		ToolCall:        extension.ToolCallMutation{Arguments: nil},
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
