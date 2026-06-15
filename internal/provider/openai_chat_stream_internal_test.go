package provider

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/llm"
)

func TestParseOpenAIChatStreamTextThinkingToolCallsAndUsage(t *testing.T) {
	t.Parallel()

	stream := openAIChatStream(
		openAIChatDelta(map[string]any{"reasoning_content": "think "}, "", nil),
		openAIChatDelta(map[string]any{jsonContentKey: "hello "}, "", nil),
		openAIChatDelta(map[string]any{"reasoning": "fallback"}, "", nil),
		openAIChatDelta(map[string]any{jsonToolCallsKey: []any{map[string]any{
			jsonIndexKey: 0,
			"id":         "call_1",
			"type":       functionToolType,
			jsonFunctionKey: map[string]any{
				jsonToolNameKey:  jsonReadToolName,
				jsonArgumentsKey: `{"pa`,
			},
		}}}, "", nil),
		openAIChatDelta(map[string]any{
			jsonContentKey: "world",
			jsonToolCallsKey: []any{map[string]any{
				jsonIndexKey: 0,
				jsonFunctionKey: map[string]any{
					jsonArgumentsKey: `th":"README.md"}`,
				},
			}},
		}, jsonToolCallsKey, map[string]any{"prompt_tokens": 4, "completion_tokens": 2}),
		openAIChatDoneLine,
	)

	events := []llm.Part{}
	result, err := parseOpenAIChatStream(strings.NewReader(stream), func(chunk *llm.StreamChunk) {
		if chunk.Part != nil {
			events = append(events, *chunk.Part)
		}
	})

	require.NoError(t, err)
	assert.Equal(t, "hello world", result.Text)
	assert.Equal(t, []string{"think ", "fallback"}, result.Thinking)
	assert.Equal(t, llm.FinishReasonToolCalls, result.FinishReason)
	assert.Equal(t, 4, result.Usage.InputTokens)
	assert.Equal(t, 2, result.Usage.OutputTokens)
	require.Len(t, result.ToolCalls, 1)
	assert.Equal(t, "call_1", result.ToolCalls[0].ID)
	assert.Equal(t, expectedReadToolName, result.ToolCalls[0].Name)
	assert.JSONEq(t, testToolArgumentsJSON, result.ToolCalls[0].ArgumentsJSON)
	assert.Equal(t, testToolPath, result.ToolCalls[0].Arguments[jsonPathKey])
	require.Len(t, events, 4)
	assert.Equal(t, llm.PartReasoning, events[0].Type)
	assert.Equal(t, llm.PartText, events[1].Type)
}

func TestParseOpenAIChatStreamHandlesErrorsAndIncompleteStreams(t *testing.T) {
	t.Parallel()

	badChatStream := openAIChatStream(openAIChatChunk(map[string]any{
		anthropicErrorEvent: map[string]any{jsonMessageKey: "bad chat"},
	}))
	_, err := parseOpenAIChatStream(strings.NewReader(badChatStream), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad chat")

	incompleteStream := openAIChatStream(
		openAIChatDelta(map[string]any{jsonContentKey: testProviderPartialText}, "", nil),
	)
	_, err = parseOpenAIChatStream(strings.NewReader(incompleteStream), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed before completion")

	doneOnlyStream := openAIChatStream(openAIChatDoneLine)
	_, err = parseOpenAIChatStream(strings.NewReader(doneOnlyStream), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed before completion")
}

func TestParseOpenAIChatStreamMapsFinishReasonLength(t *testing.T) {
	t.Parallel()

	stream := openAIChatStream(openAIChatDelta(map[string]any{jsonContentKey: testProviderPartialText}, "length", nil))
	result, err := parseOpenAIChatStream(strings.NewReader(stream), nil)

	require.NoError(t, err)
	assert.Equal(t, testProviderPartialText, result.Text)
	assert.Equal(t, llm.FinishReasonLength, result.FinishReason)
}
