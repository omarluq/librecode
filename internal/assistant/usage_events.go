package assistant

import (
	"context"

	"github.com/omarluq/librecode/internal/model"
)

func (runtime *Runtime) emitUsage(ctx context.Context, onEvent func(StreamEvent), usage model.TokenUsage) {
	if !usage.HasAny() {
		return
	}
	emitStreamEvent(onEvent, StreamEvent{
		ToolEvent: nil,
		Usage:     &usage,
		Kind:      StreamEventUsage,
		Text:      "",
	})
	payload := map[string]any{
		"context_window":    usage.ContextWindow,
		"context_tokens":    usage.ContextTokens,
		"input_tokens":      usage.InputTokens,
		jsonOutputTokensKey: usage.OutputTokens,
	}
	runtime.emit(ctx, "usage", payload)
	if runtime.extensions != nil {
		if err := runtime.extensions.Emit(ctx, "usage", payload); err != nil {
			runtime.logger.Debug("emit usage extension event failed", "error", err)
		}
	}
}
