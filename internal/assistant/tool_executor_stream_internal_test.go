package assistant

import (
	"context"
	"testing"

	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/tool"
)

func TestExecuteProviderToolCallEmitsStructuredStartAfterMutation(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	writeToolExecutorReadFixture(t, directory)

	runtime := newToolExecutorTestRuntime(mutatingToolCallLifecycle{
		failingToolResultLifecycle: failingToolResultLifecycle{},
	})
	registry := tool.NewRegistry(directory)
	events := []StreamEvent{}

	result := runtime.executeProviderToolCall(
		context.Background(),
		registry,
		&ToolCall{
			Metadata:      nil,
			Arguments:     testutil.ToolArguments(map[string]any{jsonPathKey: "stale.md"}),
			ID:            toolExecutorCallID,
			Name:          jsonReadToolName,
			ArgumentsJSON: `{"path":"stale.md"}`,
		},
		func(event StreamEvent) { events = append(events, event) },
	)

	require.False(t, result.IsError)
	require.Len(t, events, 2)
	require.Equal(t, StreamEventToolStart, events[0].Kind)
	require.NotNil(t, events[0].ToolCallEvent)
	assert.JSONEq(t, toolExecutorReadArgs, events[0].ToolCallEvent.ArgumentsJSON)
	assert.Equal(t, toolExecutorReadPath, testutil.ToolArgumentFields(events[0].ToolCallEvent.Arguments)[jsonPathKey])
	assert.Equal(t, StreamEventToolResult, events[1].Kind)
}

func TestExecuteProviderToolCallDoesNotEmitStartWhenLifecycleRejects(t *testing.T) {
	t.Parallel()

	runtime := newToolExecutorTestRuntime(failingToolCallLifecycle{})
	events := []StreamEvent{}

	result := runtime.executeProviderToolCall(
		context.Background(),
		tool.NewRegistry(t.TempDir()),
		&ToolCall{
			Metadata:      nil,
			Arguments:     testutil.ToolArguments(map[string]any{jsonPathKey: toolExecutorReadPath}),
			ID:            toolExecutorCallID,
			Name:          jsonReadToolName,
			ArgumentsJSON: toolExecutorReadArgs,
		},
		func(event StreamEvent) { events = append(events, event) },
	)

	assert.True(t, result.IsError)
	require.Len(t, events, 1)
	assert.Equal(t, StreamEventToolResult, events[0].Kind)
}

type mutatingToolCallLifecycle struct {
	failingToolResultLifecycle
}

func (mutatingToolCallLifecycle) DispatchLifecycle(
	_ context.Context,
	event extension.LifecycleEvent,
) (extension.LifecycleDispatchResult, error) {
	result := emptyTestLifecycleDispatchResult(event)
	if event.Name == extension.LifecycleToolCall {
		result.ToolCall = extension.ToolCallMutation{
			Arguments: testutil.ToolArguments(map[string]any{jsonPathKey: toolExecutorReadPath}),
			HasArgs:   true,
		}
	}

	return result, nil
}
