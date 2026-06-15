package provider

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/llm"
)

const (
	testAnthropicToolUseID = "toolu_1"
	missingFileToolError   = "missing file"
)

func TestParseAnthropicStreamExtractsNativeToolUse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		toolName string
		wantName string
	}{
		{name: "local tool name", toolName: jsonReadToolName, wantName: jsonReadToolName},
		{name: "claude code tool name", toolName: anthropicReadToolName, wantName: jsonReadToolName},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			stream := strings.Join([]string{
				anthropicEventMessageStart,
				anthropicDataLine(map[string]any{
					jsonTypeKey:    anthropicMessageStartEvent,
					jsonMessageKey: map[string]any{jsonUsageKey: map[string]any{jsonInputTokensKey: 12}},
				}),
				"",
				"event: content_block_start",
				anthropicToolUseBlockData(0, testAnthropicToolUseID, testCase.toolName, map[string]any{
					jsonPathKey: testToolPath,
				}),
				"",
				anthropicEventMessageDelta,
				anthropicDataLine(map[string]any{
					jsonTypeKey: anthropicMessageDeltaEvent,
					anthropicDeltaKey: map[string]any{
						anthropicStopReasonKey: anthropicToolUseType,
					},
					jsonUsageKey: map[string]any{jsonOutputTokensKey: 3},
				}),
				"",
				anthropicEventMessageStop,
				anthropicMessageStopData,
				"",
			}, "\n")

			result, err := parseAnthropicStream(strings.NewReader(stream), nil)
			require.NoError(t, err)
			require.Len(t, result.ToolCalls, 1)
			assert.Equal(t, testAnthropicToolUseID, result.ToolCalls[0].ID)
			assert.Equal(t, testCase.wantName, result.ToolCalls[0].Name)
			assert.Equal(t, "README.md", result.ToolCalls[0].Arguments[jsonPathKey])
			assert.Equal(t, 12, result.Usage.InputTokens)
			assert.Equal(t, 3, result.Usage.OutputTokens)
		})
	}
}

func TestAnthropicPayloadIncludesTools(t *testing.T) {
	t.Parallel()

	request := testCompletionRequestAuth("sk-ant-api03-secret")
	payload := anthropicPayload(request, nil)

	tools, ok := payload["tools"].([]map[string]any)
	require.True(t, ok)
	require.NotEmpty(t, tools)
	encoded, err := json.Marshal(tools)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"input_schema"`)
	assert.Contains(t, string(encoded), `"`+jsonReadToolName+`"`)
	assert.Contains(t, string(encoded), `"eager_input_streaming":true`)
}

func TestAnthropicToolResultMessageUsesToolUseID(t *testing.T) {
	t.Parallel()

	message, err := anthropicToolResultMessage(
		[]ToolCall{{
			Arguments:     nil,
			Metadata:      nil,
			ID:            testAnthropicToolUseID,
			Name:          jsonReadToolName,
			ArgumentsJSON: `{}`,
		}},
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

	blocks, ok := message[jsonContentKey].([]map[string]any)
	require.True(t, ok)
	require.Len(t, blocks, 1)
	assert.Equal(t, testAnthropicToolUseID, blocks[0]["tool_use_id"])
	assert.Equal(t, "ok", blocks[0][jsonContentKey])
	assert.NotContains(t, blocks[0], "is_error")
}

func TestAnthropicToolResultMessageMarksToolErrors(t *testing.T) {
	t.Parallel()

	message, err := anthropicToolResultMessage(
		[]ToolCall{{
			Arguments:     nil,
			Metadata:      nil,
			ID:            testAnthropicToolUseID,
			Name:          jsonReadToolName,
			ArgumentsJSON: `{}`,
		}},
		[]ToolEvent{{
			Name:          jsonReadToolName,
			ArgumentsJSON: `{}`,
			DetailsJSON:   "",
			Result:        missingFileToolError,
			Error:         missingFileToolError,
			IsError:       true,
		}},
	)

	require.NoError(t, err)

	blocks, ok := message[jsonContentKey].([]map[string]any)
	require.True(t, ok)
	require.Len(t, blocks, 1)
	assert.Equal(t, true, blocks[0]["is_error"])
}

func TestAnthropicToolResultMessageRejectsMismatchedCallsAndEvents(t *testing.T) {
	t.Parallel()

	message, err := anthropicToolResultMessage(
		[]ToolCall{{
			Arguments:     nil,
			Metadata:      nil,
			ID:            testAnthropicToolUseID,
			Name:          jsonReadToolName,
			ArgumentsJSON: `{}`,
		}},
		nil,
	)

	require.Error(t, err)
	assert.Nil(t, message)
	assert.Contains(t, err.Error(), "mismatched tool calls and results")
}

func TestAppendAnthropicToolConversationRejectsMismatchedNativeResults(t *testing.T) {
	t.Parallel()

	state := &anthropicLoopState{result: nil, endpoint: "", messages: nil}
	result := &providerResult{
		FinishReason: llm.FinishReasonToolCalls,
		Text:         "",
		OutputItems:  nil,
		Thinking:     nil,
		ToolCalls: []ToolCall{{
			Arguments:     nil,
			Metadata:      nil,
			ID:            testAnthropicToolUseID,
			Name:          jsonReadToolName,
			ArgumentsJSON: `{}`,
		}},
		Usage: llm.EmptyUsage(),
	}

	err := appendAnthropicToolConversation(testCompletionRequestAuth("sk-ant-api03-secret"), state, result, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "mismatched tool calls and results")
}
