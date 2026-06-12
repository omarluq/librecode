package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/llm"
)

const (
	testCallID   = "call-1"
	testToolPath = "README.md"
)

func TestValidateToolCallsRejectsMissingFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		call ToolCall
	}{
		{
			name: "missing id",
			call: ToolCall{
				Arguments:     nil,
				Metadata:      nil,
				ID:            "",
				Name:          jsonReadToolName,
				ArgumentsJSON: "",
				TextFallback:  false,
			},
		},
		{
			name: "missing name",
			call: ToolCall{
				Arguments:     nil,
				Metadata:      nil,
				ID:            testCallID,
				Name:          "",
				ArgumentsJSON: "",
				TextFallback:  false,
			},
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

func TestExecuteToolCallsRequiresExecutor(t *testing.T) {
	t.Parallel()

	outputs, events, err := executeToolCalls(
		context.Background(),
		emptyCompletionRequest(),
		[]ToolCall{readToolCall(testCallID)},
	)

	require.Error(t, err)
	assert.Nil(t, outputs)
	assert.Nil(t, events)
}

func TestExecuteToolCallsWrapsExecutorErrors(t *testing.T) {
	t.Parallel()

	request := emptyCompletionRequest()
	request.ExecuteTools = func(
		_ context.Context,
		_ []llm.ToolCall,
		_ func(*llm.StreamChunk),
	) ([]llm.ToolResult, error) {
		return nil, errors.New("boom")
	}

	outputs, events, err := executeToolCalls(
		context.Background(),
		request,
		[]ToolCall{readToolCall(testCallID)},
	)

	require.Error(t, err)
	assert.Nil(t, outputs)
	assert.Nil(t, events)
	var coded oops.OopsError
	require.ErrorAs(t, err, &coded)
	assert.Equal(t, "tool_execution_failed", coded.Code())
	assert.Equal(t, "provider", coded.Domain())
}

func TestExecuteToolCallsUsesInjectedExecutorAndHandlesMissingEvents(t *testing.T) {
	t.Parallel()

	request := emptyCompletionRequest()
	request.ExecuteTools = func(
		_ context.Context,
		calls []llm.ToolCall,
		_ func(*llm.StreamChunk),
	) ([]llm.ToolResult, error) {
		require.Len(t, calls, 1)
		assert.Equal(t, testCallID, calls[0].ID)
		return []llm.ToolResult{}, nil
	}

	outputs, events, err := executeToolCalls(
		context.Background(),
		request,
		[]ToolCall{readToolCall(testCallID)},
	)

	require.NoError(t, err)
	assert.Empty(t, events)
	require.Len(t, outputs, 1)
	assert.Equal(t, map[string]any{
		jsonTypeKey:   functionCallOutputType,
		jsonCallIDKey: testCallID,
		jsonOutputKey: "",
	}, outputs[0])
}

func TestToolCallMetadataMarksTextFallback(t *testing.T) {
	t.Parallel()

	call := readToolCall(testCallID)
	call.Metadata = map[string]any{testExistingKey: true}
	call.TextFallback = true
	metadata := toolCallMetadata(call)

	assert.Equal(t, map[string]any{testExistingKey: true, "text_fallback": true}, metadata)
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

	messages, err := openAIChatToolMessages([]ToolCall{readToolCall("call_1")}, nil)

	require.Error(t, err)
	assert.Nil(t, messages)
	assert.Contains(t, err.Error(), "mismatched tool calls and results")
}

func TestOpenAIChatToolMessagesUsesCallIDs(t *testing.T) {
	t.Parallel()

	messages, err := openAIChatToolMessages(
		[]ToolCall{readToolCall("call_1")},
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
	assert.JSONEq(t, jsonString(jsonToolRole), jsonString(messages[0][jsonRoleKey]))
	assert.Equal(t, "call_1", messages[0]["tool_call_id"])
	assert.Equal(t, "ok", messages[0][jsonContentKey])
}

func readToolCall(callID string) ToolCall {
	return ToolCall{
		Arguments:     nil,
		Metadata:      nil,
		ID:            callID,
		Name:          jsonReadToolName,
		ArgumentsJSON: `{}`,
		TextFallback:  false,
	}
}
