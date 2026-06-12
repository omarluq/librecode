package provider

import (
	"bufio"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSSEAccumulatorAddsArgumentsBeforeItem(t *testing.T) {
	t.Parallel()

	accumulator := newSSEAccumulator()
	accumulator.add(map[string]any{
		jsonTypeKey:  "response.function_call_arguments.delta",
		sseItemIDKey: testCallID,
		"arguments":  testToolArgumentsJSON,
	}, nil)

	assert.Len(t, accumulator.items, 1)
	item, ok := accumulator.items[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, testCallID, item["id"])
	assert.Equal(t, functionCallType, item[jsonTypeKey])
	argumentsJSON, ok := item[jsonArgumentsKey].(string)
	require.True(t, ok)
	assert.JSONEq(t, testToolArgumentsJSON, argumentsJSON)
}

func TestSSEAccumulatorUpsertsItemsByID(t *testing.T) {
	t.Parallel()

	accumulator := newSSEAccumulator()
	accumulator.addItem(map[string]any{"id": testCallID, jsonTypeKey: functionCallType})
	accumulator.addItem(map[string]any{
		"id":            testCallID,
		jsonTypeKey:     functionCallType,
		jsonToolNameKey: jsonReadToolName,
	})
	accumulator.addItem(map[string]any{jsonTypeKey: jsonMessageType})

	assert.Len(t, accumulator.items, 2)
	first, ok := accumulator.items[0].(map[string]any)
	require.True(t, ok)
	assert.JSONEq(t, jsonString(jsonReadToolName), jsonString(first[jsonToolNameKey]))
}

func TestSSEItemIDSources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		event map[string]any
		name  string
		want  string
	}{
		{name: "item id", event: map[string]any{sseItemIDKey: "item-1"}, want: "item-1"},
		{name: "output item id", event: map[string]any{sseOutputItemIDKey: "item-2"}, want: "item-2"},
		{name: "id", event: map[string]any{"id": "item-3"}, want: "item-3"},
		{name: "nested item", event: map[string]any{"item": map[string]any{"id": "item-4"}}, want: "item-4"},
		{name: "missing", event: map[string]any{}, want: ""},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, sseItemID(testCase.event))
		})
	}
}

func TestParseSSEResultUsesFallbackDeltasWhenFinalResponseHasNoText(t *testing.T) {
	t.Parallel()

	stream := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		completedSSEEvent(),
		``,
	}, "\n")

	result, err := parseSSEResult(strings.NewReader(stream), nil)

	require.NoError(t, err)
	assert.Equal(t, "hello", result.Text)
}

func TestScanSSEResponseReportsScannerErrors(t *testing.T) {
	t.Parallel()

	_, err := scanSSEResponse(bufio.NewScanner(errorReader{}), nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "read provider stream")
}

func TestDeltaTextFromSSEEventAcceptsTextField(t *testing.T) {
	t.Parallel()

	text, ok := deltaTextFromSSEEvent(map[string]any{"text": "delta text"})

	assert.True(t, ok)
	assert.Equal(t, "delta text", text)
}

func TestSSELineAndDecodeIgnoreInvalidInput(t *testing.T) {
	t.Parallel()

	event, found := eventFromSSELine("event: ping")
	assert.False(t, found)
	assert.Nil(t, event)
	event, found = eventFromSSELine("data: [DONE]")
	assert.False(t, found)
	assert.Nil(t, event)
	event, found = decodeEvent([]byte(`{"type":`))
	assert.False(t, found)
	assert.Nil(t, event)
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

var _ io.Reader = errorReader{}
