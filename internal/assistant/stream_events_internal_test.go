package assistant

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEmitStreamEventSkipsNilHandler(t *testing.T) {
	t.Parallel()

	require.NotPanics(t, func() {
		emitStreamEvent(nil, StreamEvent{
			ToolCallEvent: nil,
			ToolEvent:     nil,
			Usage:         nil,
			Kind:          StreamEventUnknown,
			Text:          "ignored",
		})
	})
}

func TestEmitStreamEventCallsHandler(t *testing.T) {
	t.Parallel()

	var got StreamEvent

	emitStreamEvent(func(event StreamEvent) { got = event }, StreamEvent{
		ToolCallEvent: nil,
		ToolEvent:     nil,
		Usage:         nil,
		Kind:          StreamEventTextDelta,
		Text:          "hello",
	})

	require.Equal(t, StreamEventTextDelta, got.Kind)
	require.Equal(t, "hello", got.Text)
}
