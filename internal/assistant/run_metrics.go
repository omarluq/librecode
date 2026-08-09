package assistant

import (
	"context"
	"math"
	"sync"

	"github.com/samber/oops"

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
	ProviderRoundTrips  int64
	InputTokens         int64
	OutputTokens        int64
	ToolCalls           int
	NestedToolCalls     int
	UsageTotalsReported bool
	TraceComplete       bool
}

// RunMetrics collects request-local provider, usage, and tool-trace observations.
type RunMetrics struct {
	usageTotalsErr      error
	results             map[string]struct{}
	observer            func(model.UsageTotals)
	starts              map[string]ToolCallEvent
	inputTokens         int64
	outputTokens        int64
	providerRoundTrips  int64
	toolCalls           int
	nestedToolCalls     int
	mu                  sync.Mutex
	usageTotalsReported bool
	traceValid          bool
	initialized         bool
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

	if metrics.providerRoundTrips == math.MaxInt64 {
		metrics.usageTotalsErr = oops.In("assistant").
			Code("provider_round_trips_overflow").
			Errorf("provider round trips overflow int64")
	} else {
		metrics.providerRoundTrips++
	}
	metrics.mu.Unlock()
}

func observeProviderUsage(ctx context.Context, usage model.TokenUsage) {
	metrics := runMetricsFromContext(ctx)
	if metrics == nil {
		return
	}

	observation, err := model.UsageTotalsFromTokenUsage(usage)

	var reportAccepted bool

	metrics.mu.Lock()
	metrics.initialize()

	if err != nil {
		metrics.usageTotalsErr = err
	} else if observation.Reported {
		observation.ProviderRoundTrips = 0
		current := metrics.usageTotalsLocked()

		combined, addErr := current.Add(observation)
		if addErr != nil {
			metrics.usageTotalsErr = addErr
		} else {
			metrics.inputTokens = combined.InputTokens
			metrics.outputTokens = combined.OutputTokens
			metrics.usageTotalsReported = combined.Reported
			reportAccepted = true
		}
	}

	observer := metrics.observer
	snapshot := metrics.usageTotalsLocked()
	metrics.mu.Unlock()

	if observer != nil && reportAccepted {
		observer(snapshot)
	}
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
	case StreamEventUsage, StreamEventUsageSnapshot, StreamEventUsageTotal:
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
			ToolCalls: 0, NestedToolCalls: 0, UsageTotalsReported: false, TraceComplete: true,
		}
	}

	metrics.mu.Lock()
	defer metrics.mu.Unlock()

	metrics.initialize()

	return RunMetricsSnapshot{
		ProviderRoundTrips:  metrics.providerRoundTrips,
		InputTokens:         metrics.inputTokens,
		OutputTokens:        metrics.outputTokens,
		ToolCalls:           metrics.toolCalls,
		NestedToolCalls:     metrics.nestedToolCalls,
		UsageTotalsReported: metrics.usageTotalsReported,
		TraceComplete:       metrics.traceValid && len(metrics.starts) == len(metrics.results),
	}
}

// ProviderRoundTrips returns the observed provider request count.
func (metrics *RunMetrics) ProviderRoundTrips() int64 {
	return metrics.Snapshot().ProviderRoundTrips
}

// UsageTotals returns the cumulative provider usage snapshot and any accounting error.
func (metrics *RunMetrics) UsageTotals() (model.UsageTotals, error) {
	if metrics == nil {
		return model.UsageTotals{InputTokens: 0, OutputTokens: 0, ProviderRoundTrips: 0, Reported: false}, nil
	}

	metrics.mu.Lock()
	defer metrics.mu.Unlock()

	metrics.initialize()

	return metrics.usageTotalsLocked(), metrics.usageTotalsErr
}

// SetUsageTotalsObserver installs a callback invoked after every successfully
// accounted provider response. The callback runs without the metrics lock held.
func (metrics *RunMetrics) SetUsageTotalsObserver(observer func(model.UsageTotals)) {
	if metrics == nil {
		return
	}

	metrics.mu.Lock()
	metrics.observer = observer
	metrics.mu.Unlock()
}

func (metrics *RunMetrics) usageTotalsLocked() model.UsageTotals {
	return model.UsageTotals{
		InputTokens: metrics.inputTokens, OutputTokens: metrics.outputTokens,
		ProviderRoundTrips: metrics.providerRoundTrips, Reported: metrics.usageTotalsReported,
	}
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
