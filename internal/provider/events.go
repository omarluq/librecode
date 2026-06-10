package provider

import "github.com/omarluq/librecode/internal/llm"

// StreamEventKind identifies incremental provider/client activity.
type StreamEventKind string

const (
	// StreamEventTextDelta carries assistant text as it arrives.
	StreamEventTextDelta StreamEventKind = "text_delta"
	// StreamEventThinkingDelta carries model thinking/reasoning text as it arrives.
	StreamEventThinkingDelta StreamEventKind = "thinking_delta"
	// StreamEventToolStart announces a tool call before execution.
	StreamEventToolStart StreamEventKind = "tool_start"
	// StreamEventToolResult carries the completed tool call result.
	StreamEventToolResult StreamEventKind = "tool_result"
)

// StreamEvent is emitted while the provider client is producing a result.
type StreamEvent struct {
	ToolEvent *ToolEvent      `json:"tool_event,omitempty"`
	Usage     *llm.Usage      `json:"usage,omitempty"`
	Kind      StreamEventKind `json:"kind"`
	Text      string          `json:"text,omitempty"`
}

func streamChunkToLLM(event StreamEvent) *llm.StreamChunk {
	return &llm.StreamChunk{
		Part:         streamPartToLLM(event),
		ToolCall:     nil,
		FinishReason: llm.FinishReasonUnknown,
		Usage:        usagePointerToLLM(event.Usage),
	}
}

func streamPartToLLM(event StreamEvent) *llm.Part {
	switch event.Kind {
	case StreamEventTextDelta:
		part := llm.TextPart(event.Text)
		return &part
	case StreamEventToolStart:
		call := llm.ToolCall{
			Metadata:      nil,
			Arguments:     nil,
			ID:            "",
			Name:          event.Text,
			ArgumentsJSON: "",
		}
		part := llm.Part{
			Metadata:   nil,
			ToolCall:   &call,
			ToolResult: nil,
			Type:       llm.PartToolCall,
			Text:       "",
			Data:       "",
			MIMEType:   "",
		}
		return &part
	case StreamEventThinkingDelta:
		part := llm.Part{
			Metadata:   nil,
			ToolCall:   nil,
			ToolResult: nil,
			Type:       llm.PartReasoning,
			Text:       event.Text,
			Data:       "",
			MIMEType:   "",
		}
		return &part
	case StreamEventToolResult:
		if event.ToolEvent == nil {
			return nil
		}
		part := toolResultPartFromEvent(event.ToolEvent)
		return &part
	}

	return nil
}

func usagePointerToLLM(usage *llm.Usage) llm.Usage {
	if usage == nil {
		return llm.EmptyUsage()
	}

	return *usage
}
