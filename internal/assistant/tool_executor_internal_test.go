package assistant

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/tool"
)

const toolExecutorReadArgs = `{"path":"README.md"}`

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
			ID:            "call_1",
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
			Arguments:     map[string]any{jsonPathKey: "README.md"},
			ID:            "call_1",
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

func newToolExecutorTestRuntime(extensions runtimeExtensions) *Runtime {
	return NewRuntime(&RuntimeOptions{
		Config:     nil,
		Sessions:   nil,
		Extensions: extensions,
		Cache:      nil,
		Events:     nil,
		Models:     nil,
		Client:     nil,
		Logger:     nil,
	})
}

type failingToolCallLifecycle struct{}

func (failingToolCallLifecycle) ExecuteCommand(context.Context, string, string) (string, error) {
	return "", nil
}

func (failingToolCallLifecycle) Emit(context.Context, string, map[string]any) error {
	return nil
}

func (failingToolCallLifecycle) ExecuteTool(context.Context, string, map[string]any) (extension.ToolResult, error) {
	return extension.ToolResult{Details: nil, Content: ""}, nil
}

func (failingToolCallLifecycle) Tools() []extension.Tool {
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
