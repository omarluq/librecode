package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/llm"
)

func TestStreamChunkToLLMUsesTypedToolStartPart(t *testing.T) {
	t.Parallel()

	chunk := streamChunkToLLM(StreamEvent{
		ToolEvent: nil,
		Usage:     nil,
		Kind:      StreamEventToolStart,
		Text:      jsonBashToolName,
	})
	require.NotNil(t, chunk)
	require.NotNil(t, chunk.Part)
	require.NotNil(t, chunk.Part.ToolCall)
	assert.Equal(t, llm.PartToolCall, chunk.Part.Type)
	assert.Equal(t, jsonBashToolName, chunk.Part.ToolCall.Name)
	assert.Empty(t, chunk.Part.Text)
}
