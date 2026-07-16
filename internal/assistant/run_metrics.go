package assistant

import (
	"context"
	"sync"

	"github.com/omarluq/librecode/internal/model"
)

// ToolStrategy controls which provider-facing tool surfaces are available.
type ToolStrategy string

const (
	// ToolStrategyHybrid exposes direct tools and the execute code-mode tool.
	ToolStrategyHybrid ToolStrategy = "hybrid"
	// ToolStrategyDirect exposes direct tools without execute.
	ToolStrategyDirect ToolStrategy = "direct"
)

type toolStrategyContextKey struct{}
type runMetricsContextKey struct{}

// WithToolStrategy returns a context that selects a provider-facing tool strategy.
func WithToolStrategy(ctx context.Context, strategy ToolStrategy) context.Context {
	return context.WithValue(ctx, toolStrategyContextKey{}, strategy)
}

func toolStrategyFromContext(ctx context.Context) ToolStrategy {
	strategy, ok := ctx.Value(toolStrategyContextKey{}).(ToolStrategy)
	if !ok || strategy == "" {
		return ToolStrategyHybrid
	}

	return strategy
}

// RunMetricsSnapshot is a stable point-in-time view of prompt execution metrics.
type RunMetricsSnapshot struct {
	ProviderRoundTrips int
	InputTokens        int
	OutputTokens       int
	ToolCalls          int
	NestedToolCalls    int
	TraceComplete      bool
}

// RunMetrics collects request-local provider, usage, and tool-trace observations.
type RunMetrics struct {
	starts             map[string]ToolCallEvent
	results            map[string]struct{}
	providerRoundTrips int
	inputTokens        int
	outputTokens       int
	toolCalls          int
	nestedToolCalls    int
	traceValid         bool
	initialized        bool
	mu                 sync.Mutex
}

// WithRunMetrics returns a context that records prompt execution metrics.
func WithRunMetrics(ctx context.Context, metrics *RunMetrics) context.Context {
	if metrics == nil {
		return ctx
	}

	return context.WithValue(ctx, runMetricsContextKey{}, metrics)
}

func runMetricsFromContext(ctx context.Context) *RunMetrics {
	metrics, ok := ctx.Value(runMetricsContextKey{}).(*RunMetrics)
	if !ok {
		return nil
	}

	return metrics
}

func observeProviderRoundTrip(ctx context.Context) {
	metrics := runMetricsFromContext(ctx)
	if metrics == nil {
		return
	}

	metrics.mu.Lock()
	metrics.initialize()
	metrics.providerRoundTrips++
	metrics.mu.Unlock()
}

func observeProviderUsage(ctx context.Context, usage model.TokenUsage) {
	metrics := runMetricsFromContext(ctx)
	if metrics == nil {
		return
	}

	metrics.mu.Lock()
	metrics.initialize()
	metrics.inputTokens = usage.InputTokens
	metrics.outputTokens = usage.OutputTokens
	metrics.mu.Unlock()
}

// ObserveStreamEvent records tool starts and results for trace accounting.
func (metrics *RunMetrics) ObserveStreamEvent(event StreamEvent) {
	if metrics == nil {
		return
	}

	metrics.mu.Lock()
	defer metrics.mu.Unlock()

	metrics.initialize()

	switch event.Kind {
	case StreamEventToolStart:
		metrics.observeToolStart(event.ToolCallEvent)
	case StreamEventToolResult:
		metrics.observeToolResult(event.ToolEvent)
	case StreamEventUsage, StreamEventUsageSnapshot:
		metrics.observeUsage(event.Usage)
	case StreamEventTextDelta,
		StreamEventThinkingDelta,
		StreamEventSkillLoaded,
		StreamEventContextCompaction,
		StreamEventContextCompactionStart,
		StreamEventContextCompactionDone,
		StreamEventContextCompactionError,
		StreamEventUnknown:
	}
}

func (metrics *RunMetrics) observeUsage(usage *model.TokenUsage) {
	if usage == nil {
		return
	}

	metrics.inputTokens = usage.InputTokens
	metrics.outputTokens = usage.OutputTokens
}

func (metrics *RunMetrics) observeToolStart(event *ToolCallEvent) {
	if event == nil || event.ID == "" {
		metrics.traceValid = false

		return
	}

	if _, exists := metrics.starts[event.ID]; exists {
		metrics.traceValid = false

		return
	}

	metrics.starts[event.ID] = *event
}

func (metrics *RunMetrics) observeToolResult(event *ToolEvent) {
	metrics.toolCalls++
	if event == nil || event.CallID == "" {
		metrics.traceValid = false

		return
	}

	if event.ParentCallID != "" {
		metrics.nestedToolCalls++
	}

	start, exists := metrics.starts[event.CallID]

	_, duplicate := metrics.results[event.CallID]
	if !exists || duplicate || start.ParentCallID != event.ParentCallID || start.Sequence != event.Sequence {
		metrics.traceValid = false
	}

	metrics.results[event.CallID] = struct{}{}
}

// Snapshot returns a concurrency-safe copy of the collected metrics.
func (metrics *RunMetrics) Snapshot() RunMetricsSnapshot {
	if metrics == nil {
		return RunMetricsSnapshot{
			ProviderRoundTrips: 0, InputTokens: 0, OutputTokens: 0,
			ToolCalls: 0, NestedToolCalls: 0, TraceComplete: true,
		}
	}

	metrics.mu.Lock()
	defer metrics.mu.Unlock()

	metrics.initialize()

	return RunMetricsSnapshot{
		ProviderRoundTrips: metrics.providerRoundTrips,
		InputTokens:        metrics.inputTokens,
		OutputTokens:       metrics.outputTokens,
		ToolCalls:          metrics.toolCalls,
		NestedToolCalls:    metrics.nestedToolCalls,
		TraceComplete:      metrics.traceValid && len(metrics.starts) == len(metrics.results),
	}
}

// ProviderRoundTrips returns the observed provider request count.
func (metrics *RunMetrics) ProviderRoundTrips() int {
	return metrics.Snapshot().ProviderRoundTrips
}

func (metrics *RunMetrics) initialize() {
	if metrics.initialized {
		return
	}

	metrics.starts = make(map[string]ToolCallEvent)
	metrics.results = make(map[string]struct{})
	metrics.traceValid = true
	metrics.initialized = true
}
