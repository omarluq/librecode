//nolint:testpackage // These tests exercise unexported tool-loop helpers.
package provider

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/tool"
)

const (
	testCallID   = "call-1"
	testToolPath = "README.md"
)

func newToolRegistryForTest(t *testing.T) *tool.Registry {
	t.Helper()

	return tool.NewRegistry(t.TempDir())
}

func TestValidateToolCallsRejectsMissingFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		call ToolCall
	}{
		{
			name: "missing id",
			call: ToolCall{Arguments: nil, ID: "", Name: jsonReadToolName, ArgumentsJSON: "", TextFallback: false},
		},
		{
			name: "missing name",
			call: ToolCall{Arguments: nil, ID: testCallID, Name: "", ArgumentsJSON: "", TextFallback: false},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateToolCalls([]ToolCall{tt.call})
			require.Error(t, err)
		})
	}
}

func TestExecuteToolCallsInvokesCallbacksAndStreamsEvents(t *testing.T) {
	t.Parallel()

	streamEvents := []StreamEvent{}
	toolCalls := []ToolCallEvent{}
	toolResults := []ToolEvent{}
	request := &CompletionRequest{
		OnEvent: func(event StreamEvent) {
			streamEvents = append(streamEvents, event)
		},
		OnProviderObserve: nil,
		OnProviderRequest: nil,
		OnToolCall: func(_ context.Context, event *ToolCallEvent) error {
			require.NotNil(t, event)
			toolCalls = append(toolCalls, *event)
			return nil
		},
		OnToolResult: func(_ context.Context, event *ToolEvent) error {
			require.NotNil(t, event)
			toolResults = append(toolResults, *event)
			return nil
		},
		ToolRegistry:    newToolRegistryForTest(t),
		ExecuteTools:    nil,
		SessionID:       "",
		SystemPrompt:    "",
		ThinkingLevel:   "",
		CWD:             t.TempDir(),
		Auth:            emptyRequestAuth(),
		Messages:        nil,
		Usage:           model.EmptyTokenUsage(),
		Model:           emptyModel(),
		ProviderAttempt: 0,
		DisableTools:    false,
	}
	outputs, events, err := executeToolCalls(context.Background(), request, []ToolCall{{
		Arguments:     map[string]any{jsonPathKey: "missing.txt"},
		ID:            testCallID,
		Name:          jsonReadToolName,
		ArgumentsJSON: `{"path":"missing.txt"}`,
		TextFallback:  false,
	}})
	require.NoError(t, err)

	require.Len(t, outputs, 1)
	require.Len(t, events, 1)
	require.Len(t, toolCalls, 1)
	require.Len(t, toolResults, 1)
	require.Len(t, streamEvents, 2)
	assert.Equal(t, StreamEventToolStart, streamEvents[0].Kind)
	assert.Equal(t, StreamEventToolResult, streamEvents[1].Kind)
	assert.Equal(t, "read", toolCalls[0].Name)
	assert.Equal(t, "read", toolResults[0].Name)
	assert.NotEmpty(t, toolResults[0].Error)
	assert.True(t, toolResults[0].IsError)
}

func TestExecuteToolCallsMarksToolCallHookErrors(t *testing.T) {
	t.Parallel()

	request := &CompletionRequest{
		OnEvent:           nil,
		OnProviderObserve: nil,
		OnProviderRequest: nil,
		OnToolCall: func(context.Context, *ToolCallEvent) error {
			return assert.AnError
		},
		OnToolResult:    nil,
		ToolRegistry:    newToolRegistryForTest(t),
		ExecuteTools:    nil,
		SessionID:       "",
		SystemPrompt:    "",
		ThinkingLevel:   "",
		CWD:             t.TempDir(),
		Auth:            emptyRequestAuth(),
		Messages:        nil,
		Usage:           model.EmptyTokenUsage(),
		Model:           emptyModel(),
		ProviderAttempt: 0,
		DisableTools:    false,
	}
	outputs, events, err := executeToolCalls(context.Background(), request, []ToolCall{{
		Arguments:     map[string]any{jsonPathKey: testToolPath},
		ID:            testCallID,
		Name:          jsonReadToolName,
		ArgumentsJSON: `{"path":"` + testToolPath + `"}`,
		TextFallback:  false,
	}})
	require.NoError(t, err)

	require.Len(t, events, 1)
	assert.Equal(t, assert.AnError.Error(), events[0].Error)
	assert.True(t, events[0].IsError)
	require.Len(t, outputs, 1)
	assert.Contains(t, outputs[0], jsonOutputKey)
}

func TestRunToolCallMarksFailedToolsAsErrors(t *testing.T) {
	t.Parallel()

	event := runToolCall(context.Background(), newToolRegistryForTest(t), ToolCallEvent{
		Arguments:     map[string]any{jsonPathKey: "missing.txt"},
		ID:            testCallID,
		Name:          jsonReadToolName,
		ArgumentsJSON: `{"path":"missing.txt"}`,
	})

	require.NotEmpty(t, event.Error)
	assert.True(t, event.IsError)
}

func TestToolOutputTextIncludesDetailsForEmptyResult(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "details:\n{}", toolOutputText("   ", "{}"))
	assert.Equal(t, "plain", toolOutputText("plain", ""))
	assert.Equal(t, "plain\ndetails:\n{}", toolOutputText(" plain ", "{}"))
}

func TestEncodeToolDetailsReturnsEmptyForInvalidDetails(t *testing.T) {
	t.Parallel()

	encoded := encodeToolDetails(map[string]any{"bad": func() {}})
	assert.Empty(t, encoded)
}

func TestOpenAIChatToolMessagesRejectsMismatchedCallsAndEvents(t *testing.T) {
	t.Parallel()

	messages, err := openAIChatToolMessages(
		[]ToolCall{{Arguments: nil, ID: "call_1", Name: jsonReadToolName, ArgumentsJSON: `{}`, TextFallback: false}},
		nil,
	)

	require.Error(t, err)
	assert.Nil(t, messages)
	assert.Contains(t, err.Error(), "mismatched tool calls and results")
}

func TestOpenAIChatToolMessagesUsesCallIDs(t *testing.T) {
	t.Parallel()

	messages, err := openAIChatToolMessages(
		[]ToolCall{{Arguments: nil, ID: "call_1", Name: jsonReadToolName, ArgumentsJSON: `{}`, TextFallback: false}},
		[]ToolEvent{{
			Name:          jsonReadToolName,
			ArgumentsJSON: `{}`,
			DetailsJSON:   "",
			Result:        "ok",
			Error:         "",
			IsError:       false,
		}},
	)

	require.NoError(t, err)
	require.Len(t, messages, 1)
	assert.Equal(t, jsonToolRole, messages[0][jsonRoleKey])
	assert.Equal(t, "call_1", messages[0]["tool_call_id"])
	assert.Equal(t, "ok", messages[0][jsonContentKey])
}
