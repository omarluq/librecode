package assistant

import (
	"context"
	"maps"
	"slices"

	"github.com/omarluq/librecode/internal/assistant/lifecyclepayload"
	"github.com/omarluq/librecode/internal/model"
)

func (runtime *Runtime) emitUsage(ctx context.Context, onEvent func(StreamEvent), usage *model.TokenUsage) {
	runtime.emitUsageEvent(ctx, onEvent, usage, StreamEventUsage)
}

func (runtime *Runtime) emitUsageSnapshot(ctx context.Context, onEvent func(StreamEvent), usage *model.TokenUsage) {
	runtime.emitUsageEvent(ctx, onEvent, usage, StreamEventUsageSnapshot)
}

func (runtime *Runtime) emitUsageEvent(
	ctx context.Context,
	onEvent func(StreamEvent),
	usage *model.TokenUsage,
	kind StreamEventKind,
) {
	if usage == nil || !usage.HasAny() {
		return
	}

	usageSnapshot := cloneTokenUsage(usage)
	emitStreamEvent(onEvent, StreamEvent{
		ToolCallEvent: nil,
		ToolEvent:     nil,
		Usage:         usageSnapshot,
		Kind:          kind,
		Text:          "",
	})

	payload := lifecyclepayload.TokenUsage(usageSnapshot)
	if kind == StreamEventUsageSnapshot {
		payload["snapshot"] = true
	}

	if runtime.extensions != nil {
		if err := runtime.extensions.Emit(ctx, lifecyclepayload.UsageKey, payload); err != nil {
			runtime.logger.Debug("emit usage extension event failed", "error", err)
		}
	}
}

func cloneTokenUsage(usage *model.TokenUsage) *model.TokenUsage {
	if usage == nil {
		return nil
	}

	cloned := *usage
	cloned.Breakdown = maps.Clone(usage.Breakdown)
	cloned.TopContributors = slices.Clone(usage.TopContributors)

	return &cloned
}
