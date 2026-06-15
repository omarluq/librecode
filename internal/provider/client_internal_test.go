package provider

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/llm"
)

const (
	answerDelta                     = "answer"
	incompleteTextTrailer           = `,"output":[],"usage":{"input_tokens":10,"output_tokens":2}}}`
	incompleteFilteredTextTrailer   = `,"output":[],"usage":{"input_tokens":7,"output_tokens":1}}}`
	incompleteMissingDetailsTrailer = `"output":[],"usage":{"input_tokens":5,"output_tokens":1}}}`
)

func TestSSEAccumulatorEmitsOutputTextDelta(t *testing.T) {
	t.Parallel()

	accumulator := newSSEAccumulator()
	events := []llm.StreamChunk{}

	accumulator.add(map[string]any{
		jsonTypeKey:       "response.output_text.delta",
		anthropicDeltaKey: answerDelta,
	}, func(event *llm.StreamChunk) {
		require.NotNil(t, event)
		events = append(events, *event)
	})

	require.Len(t, events, 1)
	require.NotNil(t, events[0].Part)
	assert.Equal(t, llm.PartText, events[0].Part.Type)
	assert.Equal(t, answerDelta, events[0].Part.Text)
	assert.Equal(t, []string{answerDelta}, accumulator.parts)
}

func TestSSEAccumulatorEmitsReasoningDeltaSeparately(t *testing.T) {
	t.Parallel()

	accumulator := newSSEAccumulator()
	events := []llm.StreamChunk{}

	accumulator.add(map[string]any{
		jsonTypeKey:       "response.reasoning_summary_text.delta",
		anthropicDeltaKey: testThinkingDelta,
	}, func(event *llm.StreamChunk) {
		require.NotNil(t, event)
		events = append(events, *event)
	})

	require.Len(t, events, 1)
	require.NotNil(t, events[0].Part)
	assert.Equal(t, llm.PartReasoning, events[0].Part.Type)
	assert.Equal(t, testThinkingDelta, events[0].Part.Text)
	assert.Empty(t, accumulator.parts)
}

func TestParseSSEResultExtractsToolCallFromOutputItems(t *testing.T) {
	t.Parallel()

	payload := `{"response":{"output":[{"id":"call_1","type":"function_call",` +
		`"call_id":"call_1","name":"read","arguments":"{\"path\":\"README.md\"}"}]}}`
	stream := "data: " + payload + "\n" + completedSSEEvent()

	result, err := parseSSEResult(strings.NewReader(stream), nil)
	require.NoError(t, err)
	require.Len(t, result.ToolCalls, 1)
	assert.Equal(t, "call_1", result.ToolCalls[0].ID)
	assert.Equal(t, "read", result.ToolCalls[0].Name)
	assert.JSONEq(t, `{"path":"README.md"}`, result.ToolCalls[0].ArgumentsJSON)
	assert.Equal(t, "README.md", result.ToolCalls[0].Arguments["path"])
}

func TestParseSSEResultFailureCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		stream            string
		expectedSubstring string
	}{
		{
			name: "failed event",
			stream: "data: " + `{"type":"response.failed",` +
				`"response":{"error":{"message":"nope"}}}` + "\n",
			expectedSubstring: "nope",
		},
		{
			name:              "closed before completion",
			stream:            "data: " + `{"type":"response.created"}` + "\n",
			expectedSubstring: "provider stream closed before completion",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := parseSSEResult(strings.NewReader(test.stream), nil)

			require.Error(t, err)
			assert.Contains(t, err.Error(), test.expectedSubstring)
		})
	}
}

func TestParseSSEResultIncompleteEvents(t *testing.T) {
	t.Parallel()

	for _, test := range incompleteSSECases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result, err := parseSSEResult(strings.NewReader(test.stream), nil)

			require.NoError(t, err)
			assert.Equal(t, test.expectedText, result.Text)
			assert.Equal(t, test.expectedFinishReason, result.FinishReason)
			assert.Equal(t, test.expectedInputTokens, result.Usage.InputTokens)
			assert.Equal(t, test.expectedOutputTokens, result.Usage.OutputTokens)
			require.Len(t, result.ToolCalls, test.expectedToolCalls)

			if test.expectedToolCalls > 0 {
				assert.Equal(t, test.expectedToolName, result.ToolCalls[0].Name)
			}
		})
	}
}

type incompleteSSECase struct {
	name                 string
	stream               string
	expectedText         string
	expectedFinishReason llm.FinishReason
	expectedToolName     string
	expectedInputTokens  int
	expectedOutputTokens int
	expectedToolCalls    int
}

func incompleteSSECases() []incompleteSSECase {
	return []incompleteSSECase{
		{
			name:                 "max output tokens returns accumulated deltas",
			stream:               incompleteTextSSE("partial ", "max_output_tokens", incompleteTextTrailer),
			expectedText:         "partial answer",
			expectedFinishReason: llm.FinishReasonLength,
			expectedToolName:     "",
			expectedInputTokens:  10,
			expectedOutputTokens: 2,
			expectedToolCalls:    0,
		},
		{
			name:                 "max output tokens preserves final finish reason with accumulated items",
			stream:               incompleteOutputItemSSE(),
			expectedText:         "partial answer",
			expectedFinishReason: llm.FinishReasonLength,
			expectedToolName:     "",
			expectedInputTokens:  10,
			expectedOutputTokens: 2,
			expectedToolCalls:    0,
		},
		{
			name:                 "content filter maps to content filter finish reason",
			stream:               incompleteTextSSE("safe ", "content_filter", incompleteFilteredTextTrailer),
			expectedText:         "safe answer",
			expectedFinishReason: llm.FinishReasonContentFilter,
			expectedToolName:     "",
			expectedInputTokens:  7,
			expectedOutputTokens: 1,
			expectedToolCalls:    0,
		},
		{
			name:                 "missing incomplete details defaults to length",
			stream:               incompleteTextSSE("truncated ", "", incompleteMissingDetailsTrailer),
			expectedText:         "truncated answer",
			expectedFinishReason: llm.FinishReasonLength,
			expectedToolName:     "",
			expectedInputTokens:  5,
			expectedOutputTokens: 1,
			expectedToolCalls:    0,
		},
		{
			name:                 "tool calls win over length",
			stream:               incompleteToolCallSSE(),
			expectedText:         "",
			expectedFinishReason: llm.FinishReasonToolCalls,
			expectedToolName:     jsonReadToolName,
			expectedInputTokens:  0,
			expectedOutputTokens: 0,
			expectedToolCalls:    1,
		},
	}
}

func incompleteTextSSE(prefix, reason, trailer string) string {
	incompleteDetails := ""
	if reason != "" {
		incompleteDetails = `"incomplete_details":{"reason":"` + reason + `"}`
	}

	return strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"` + prefix + `"}`,
		`data: {"type":"response.output_text.delta","delta":"` + answerDelta + `"}`,
		`data: {"type":"response.incomplete","response":{"status":"incomplete",` + incompleteDetails + trailer,
		``,
	}, "\n")
}

func incompleteOutputItemSSE() string {
	return strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"id":"msg_1","type":"message",` +
			`"content":[{"type":"output_text","text":"partial answer"}]}}`,
		`data: {"type":"response.incomplete","response":{"status":"incomplete",` +
			`"incomplete_details":{"reason":"max_output_tokens"}` + incompleteTextTrailer,
		``,
	}, "\n")
}

func incompleteToolCallSSE() string {
	return "data: " + `{"type":"response.incomplete","response":{"status":"incomplete",` +
		`"incomplete_details":{"reason":"max_output_tokens"},"output":[{"id":"call_1",` +
		`"type":"function_call","call_id":"call_1","name":"read",` +
		`"arguments":"{\"path\":\"README.md\"}"}]}}` + "\n"
}

func completedSSEEvent() string {
	return "data: " + `{"type":"response.completed","response":{"status":"completed","output":[]}}` + "\n"
}
