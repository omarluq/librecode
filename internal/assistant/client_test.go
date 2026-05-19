//nolint:testpackage // These tests cover unexported SSE accumulator behavior.
package assistant

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const answerDelta = "answer"

func TestSSEAccumulatorEmitsOutputTextDelta(t *testing.T) {
	t.Parallel()

	accumulator := newSSEAccumulator()
	events := []StreamEvent{}

	accumulator.add(map[string]any{
		jsonTypeKey: "response.output_text.delta",
		"delta":     answerDelta,
	}, func(event StreamEvent) {
		events = append(events, event)
	})

	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].Kind != StreamEventTextDelta {
		t.Fatalf("event kind = %q, want %q", events[0].Kind, StreamEventTextDelta)
	}
	if events[0].Text != answerDelta {
		t.Fatalf("event text = %q, want %q", events[0].Text, answerDelta)
	}
	if got := len(accumulator.parts); got != 1 || accumulator.parts[0] != answerDelta {
		t.Fatalf("accumulator parts = %#v, want [%s]", accumulator.parts, answerDelta)
	}
}

func TestSSEAccumulatorEmitsReasoningDeltaSeparately(t *testing.T) {
	t.Parallel()

	accumulator := newSSEAccumulator()
	events := []StreamEvent{}

	accumulator.add(map[string]any{
		jsonTypeKey: "response.reasoning_summary_text.delta",
		"delta":     "thinking",
	}, func(event StreamEvent) {
		events = append(events, event)
	})

	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].Kind != StreamEventThinkingDelta {
		t.Fatalf("event kind = %q, want %q", events[0].Kind, StreamEventThinkingDelta)
	}
	if events[0].Text != "thinking" {
		t.Fatalf("event text = %q, want %q", events[0].Text, "thinking")
	}
	if len(accumulator.parts) != 0 {
		t.Fatalf("reasoning deltas should not be response text parts: %#v", accumulator.parts)
	}
}
func TestParseSSEResultExtractsToolCallFromOutputItems(t *testing.T) {
	t.Parallel()

	payload := `{"response":{"output":[{"id":"call_1","type":"function_call",` +
		`"call_id":"call_1","name":"read","arguments":"{\"path\":\"README.md\"}"}]}}`
	stream := "data: " + payload + "\n" + "data: [DONE]\n"

	result, err := parseSSEResult(strings.NewReader(stream), nil)
	require.NoError(t, err)
	require.Len(t, result.ToolCalls, 1)
	assert.Equal(t, "call_1", result.ToolCalls[0].ID)
	assert.Equal(t, "read", result.ToolCalls[0].Name)
	assert.Equal(t, `{"path":"README.md"}`, result.ToolCalls[0].ArgumentsJSON)
	assert.Equal(t, "README.md", result.ToolCalls[0].Arguments["path"])
}
