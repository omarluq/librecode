package assistant

import (
	"context"

	"github.com/omarluq/librecode/internal/model"
)

func (runtime *Runtime) emitUsage(ctx context.Context, onEvent func(StreamEvent), usage model.TokenUsage) {
	runtime.emitUsageEvent(ctx, onEvent, usage, StreamEventUsage)
}

func (runtime *Runtime) emitUsageSnapshot(ctx context.Context, onEvent func(StreamEvent), usage model.TokenUsage) {
	runtime.emitUsageEvent(ctx, onEvent, usage, StreamEventUsageSnapshot)
}

func (runtime *Runtime) emitUsageEvent(
	ctx context.Context,
	onEvent func(StreamEvent),
	usage model.TokenUsage,
	kind StreamEventKind,
) {
	if !usage.HasAny() {
		return
	}
	emitStreamEvent(onEvent, StreamEvent{
		ToolEvent: nil,
		Usage:     &usage,
		Kind:      kind,
		Text:      "",
	})
	payload := map[string]any{
		jsonBreakdownKey:     cloneIntMap(usage.Breakdown),
		jsonContextWindowKey: usage.ContextWindow,
		jsonContextTokensKey: usage.ContextTokens,
		jsonInputTokensKey:   usage.InputTokens,
		jsonOutputTokensKey:  usage.OutputTokens,
	}
	if kind == StreamEventUsageSnapshot {
		payload["snapshot"] = true
	}
	runtime.emit(ctx, jsonUsageKey, payload)
	if runtime.extensions != nil {
		if err := runtime.extensions.Emit(ctx, jsonUsageKey, payload); err != nil {
			runtime.logger.Debug("emit usage extension event failed", "error", err)
		}
	}
}
