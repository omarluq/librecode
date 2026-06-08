package provider

import "github.com/omarluq/librecode/internal/model"

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
	ToolEvent *ToolEvent        `json:"tool_event,omitempty"`
	Usage     *model.TokenUsage `json:"usage,omitempty"`
	Kind      StreamEventKind   `json:"kind"`
	Text      string            `json:"text,omitempty"`
}
