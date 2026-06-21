package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/llm"
)

func TestStreamChunkToLLMMapsEventKinds(t *testing.T) {
	t.Parallel()

	usage := streamEventTestUsage()
	tests := []streamEventMappingCase{
		{
			usage: nil,
			name:  "text",
			event: StreamEvent{
				ToolEvent: nil,
				Usage:     nil,
				Kind:      StreamEventTextDelta,
				Text:      testProviderHello,
			},
			wantPart: llm.PartText,
			wantText: testProviderHello,
		},
		{
			usage: nil,
			name:  "thinking",
			event: StreamEvent{
				ToolEvent: nil,
				Usage:     nil,
				Kind:      StreamEventThinkingDelta,
				Text:      testThinkingDelta,
			},
			wantPart: llm.PartReasoning,
			wantText: testThinkingDelta,
		},
		{
			usage: nil,
			name:  "tool start",
			event: StreamEvent{
				ToolEvent: nil,
				Usage:     nil,
				Kind:      StreamEventToolStart,
				Text:      jsonBashToolName,
			},
			wantPart: llm.PartToolCall,
			wantText: "",
		},
		{
			name: "tool result",
			event: StreamEvent{
				ToolEvent: &ToolEvent{
					Name:          jsonBashToolName,
					ArgumentsJSON: `{}`,
					DetailsJSON:   "",
					Result:        "ok",
					Error:         "",
					IsError:       false,
				},
				Usage: nil,
				Kind:  StreamEventToolResult,
				Text:  "",
			},
			usage:    usage,
			wantPart: llm.PartToolResult,
			wantText: "",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			event := testCase.event
			event.Usage = testCase.usage
			chunk := streamChunkToLLM(event)
			require.NotNil(t, chunk)
			require.NotNil(t, chunk.Part)
			assert.Equal(t, testCase.wantPart, chunk.Part.Type)
			assert.Equal(t, testCase.wantText, chunk.Part.Text)

			if testCase.usage != nil {
				assert.Equal(t, 3, chunk.Usage.InputTokens)
			}
		})
	}
}

type streamEventMappingCase struct {
	usage    *llm.Usage
	event    StreamEvent
	name     string
	wantText string
	wantPart llm.PartType
}

func streamEventTestUsage() *llm.Usage {
	return &llm.Usage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   0,
		ContextTokens:   0,
		InputTokens:     3,
		OutputTokens:    0,
	}
}

func TestStreamPartToLLMHandlesMissingAndUnknownEvents(t *testing.T) {
	t.Parallel()

	assert.Nil(t, streamPartToLLM(StreamEvent{ToolEvent: nil, Usage: nil, Kind: StreamEventToolResult, Text: ""}))
	assert.Nil(t, streamPartToLLM(StreamEvent{ToolEvent: nil, Usage: nil, Kind: StreamEventKind("unknown"), Text: ""}))
	assert.False(t, usagePointerToLLM(nil).HasAny())
}
