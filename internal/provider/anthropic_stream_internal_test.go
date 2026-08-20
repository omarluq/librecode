package provider

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/llm"
)

func TestParseAnthropicStreamTextThinkingToolUseAndUsage(t *testing.T) {
	t.Parallel()

	stream := strings.Join([]string{
		anthropicEventMessageStart,
		anthropicDataLine(map[string]any{
			jsonTypeKey:    anthropicMessageStartEvent,
			jsonMessageKey: map[string]any{jsonUsageKey: map[string]any{jsonInputTokensKey: 5}},
		}),
		"",
		anthropicEventContentBlockDelta,
		anthropicDataLine(anthropicDeltaEvent(0, anthropicThinkingDelta, "thinking", "think")),
		"",
		anthropicEventContentBlockDelta,
		anthropicDataLine(anthropicDeltaEvent(1, anthropicTextDelta, jsonTextKey, "hello")),
		"",
		"event: content_block_start",
		anthropicDataLine(map[string]any{
			jsonTypeKey:  anthropicContentBlockStartEvent,
			jsonIndexKey: 2,
			"content_block": map[string]any{
				jsonTypeKey:     anthropicToolUseType,
				"id":            testAnthropicToolUseID,
				jsonToolNameKey: anthropicReadToolName,
				jsonInputKey:    map[string]any{},
			},
		}),
		"",
		anthropicEventContentBlockDelta,
		anthropicDataLine(anthropicDeltaEvent(2, anthropicInputJSONDelta, "partial_json", testToolArgumentsJSON)),
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

	events := []llm.Part{}
	result, err := parseAnthropicStream(strings.NewReader(stream), func(chunk *llm.StreamChunk) {
		if chunk.Part != nil {
			events = append(events, *chunk.Part)
		}
	})

	require.NoError(t, err)
	assert.Equal(t, "hello", result.Text)
	assert.Equal(t, []string{"think"}, result.Thinking)
	assert.Equal(t, llm.FinishReasonToolCalls, result.FinishReason)
	assert.Equal(t, 5, result.Usage.InputTokens)
	assert.Equal(t, 3, result.Usage.OutputTokens)
	require.Len(t, result.ToolCalls, 1)
	assert.Equal(t, testAnthropicToolUseID, result.ToolCalls[0].ID)
	assert.Equal(t, expectedReadToolName, result.ToolCalls[0].Name)
	assert.JSONEq(t, testToolArgumentsJSON, result.ToolCalls[0].ArgumentsJSON)
	require.Len(t, events, 2)
	assert.Equal(t, llm.PartReasoning, events[0].Type)
	assert.Equal(t, llm.PartText, events[1].Type)
}

func TestParseAnthropicStreamJoinsThinkingDeltasWithoutChangingCallbacks(t *testing.T) {
	t.Parallel()

	deltas := []string{testThinkingChunkUse, testThinkingChunkThe, " ", jsonToolRole}

	lines := make([]string, 0, len(deltas)*3+3)
	for _, delta := range deltas {
		lines = append(lines,
			anthropicEventContentBlockDelta,
			anthropicDataLine(anthropicDeltaEvent(0, anthropicThinkingDelta, "thinking", delta)),
			"",
		)
	}

	lines = append(lines, anthropicEventMessageStop, anthropicMessageStopData, "")
	streamed := []string{}

	result, err := parseAnthropicStream(strings.NewReader(strings.Join(lines, "\n")), func(chunk *llm.StreamChunk) {
		if chunk.Part != nil && chunk.Part.Type == llm.PartReasoning {
			streamed = append(streamed, chunk.Part.Text)
		}
	})

	require.NoError(t, err)
	assert.Equal(t, deltas, streamed)
	assert.Equal(t, []string{testJoinedThinkingText}, result.Thinking)
}

func TestParseAnthropicStreamStopsAtMessageStop(t *testing.T) {
	t.Parallel()

	stream := strings.Join([]string{
		anthropicEventContentBlockDelta,
		anthropicDataLine(anthropicDeltaEvent(0, anthropicThinkingDelta, "thinking", "done")),
		"",
		anthropicEventMessageDelta,
		anthropicDataLine(map[string]any{
			jsonTypeKey: anthropicMessageDeltaEvent,
			anthropicDeltaKey: map[string]any{
				anthropicStopReasonKey: anthropicStopEndTurn,
			},
		}),
		"",
		anthropicEventMessageStop,
		anthropicMessageStopData,
		"",
		"data: invalid-json",
		"",
	}, "\n")
	result, err := parseAnthropicStream(strings.NewReader(stream), nil)

	require.NoError(t, err)
	assert.Equal(t, []string{"done"}, result.Thinking)
	assert.Equal(t, llm.FinishReasonStop, result.FinishReason)
}

func TestParseAnthropicStreamHandlesErrorsRefusalsAndIncompleteStreams(t *testing.T) {
	t.Parallel()

	badStream := strings.Join([]string{
		"event: " + anthropicErrorEvent,
		anthropicDataLine(map[string]any{
			jsonTypeKey: anthropicErrorEvent,
			anthropicErrorEvent: map[string]any{
				jsonMessageType: "bad anthropic",
			},
		}),
		"",
	}, "\n")
	_, err := parseAnthropicStream(strings.NewReader(badStream), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad anthropic")

	refusalStream := strings.Join(anthropicMessageDeltaLines(anthropicRefusalReason, &anthropicStopDetails{
		Type:        "",
		Category:    "safety",
		Explanation: testProviderDeclined,
	}), "\n")
	result, err := parseAnthropicStream(strings.NewReader(refusalStream), nil)
	require.NoError(t, err)
	assert.Equal(t, llm.FinishReasonRefusal, result.FinishReason)
	assert.Equal(t, "The model refused the request (safety): declined", result.Text)

	_, err = parseAnthropicStream(strings.NewReader("event: ping\ndata: {\"type\":\"ping\"}\n\n"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed before completion")
}

func anthropicDataLine(value map[string]any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic("anthropicDataLine: failed to marshal test payload: " + err.Error())
	}

	return "data: " + string(encoded)
}

func anthropicDeltaEvent(index int, deltaType, field, value string) map[string]any {
	return map[string]any{
		jsonTypeKey:  anthropicContentBlockDeltaEvent,
		jsonIndexKey: index,
		anthropicDeltaKey: map[string]any{
			jsonTypeKey: deltaType,
			field:       value,
		},
	}
}
