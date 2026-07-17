package assistant

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/tool"
)

const metricsMalformedCall = "metrics-malformed-call"

func TestRunMetricsCollectsUsageAndNestedTrace(t *testing.T) {
	t.Parallel()

	const (
		outerCallID  = "metrics-outer"
		nestedCallID = "metrics-outer/1"
	)

	metrics := new(RunMetrics)
	ctx := WithRunMetrics(t.Context(), metrics)
	observeProviderRoundTrip(ctx)
	observeProviderRoundTrip(ctx)
	observeProviderUsage(ctx, metricsTokenUsage(13, 5))

	metrics.ObserveStreamEvent(metricsStart(outerCallID, "", 0))
	metrics.ObserveStreamEvent(metricsStart(nestedCallID, outerCallID, 1))
	metrics.ObserveStreamEvent(metricsResult(nestedCallID, outerCallID, 1))
	metrics.ObserveStreamEvent(metricsResult(outerCallID, "", 0))

	assert.Equal(t, RunMetricsSnapshot{
		ProviderRoundTrips: 2, InputTokens: 13, OutputTokens: 5,
		ToolCalls: 2, NestedToolCalls: 1, TraceComplete: true,
	}, metrics.Snapshot())
}

func TestRunMetricsDetectsIncompleteTrace(t *testing.T) {
	t.Parallel()

	metrics := new(RunMetrics)
	metrics.ObserveStreamEvent(metricsStart("metrics-incomplete", "", 0))

	assert.False(t, metrics.Snapshot().TraceComplete)
}

func TestRunMetricsContextDefaultsAndNilSafety(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	assert.Equal(t, ToolStrategyHybrid, toolStrategyFromContext(ctx))
	assert.Equal(t, ToolStrategyHybrid, toolStrategyFromContext(WithToolStrategy(ctx, "")))
	assert.Equal(t, ToolStrategyDirect, toolStrategyFromContext(WithToolStrategy(ctx, ToolStrategyDirect)))
	assert.Same(t, ctx, WithRunMetrics(ctx, nil))
	assert.Nil(t, runMetricsFromContext(ctx))

	observeProviderRoundTrip(ctx)
	observeProviderUsage(ctx, metricsTokenUsage(9, 4))

	var nilMetrics *RunMetrics

	nilMetrics.ObserveStreamEvent(metricsEvent(StreamEventToolStart, nil, nil, nil))
	assert.Equal(t, RunMetricsSnapshot{
		ProviderRoundTrips: 0, InputTokens: 0, OutputTokens: 0,
		ToolCalls: 0, NestedToolCalls: 0, TraceComplete: true,
	}, nilMetrics.Snapshot())
}

func TestRunMetricsStreamUsageAndIgnoredEvents(t *testing.T) {
	t.Parallel()

	metrics := new(RunMetrics)
	metrics.ObserveStreamEvent(metricsEvent(StreamEventUsage, nil, nil, nil))

	usage := metricsTokenUsage(21, 8)
	metrics.ObserveStreamEvent(metricsEvent(StreamEventUsageSnapshot, nil, nil, &usage))

	for _, kind := range []StreamEventKind{
		StreamEventTextDelta, StreamEventThinkingDelta, StreamEventSkillLoaded,
		StreamEventContextCompaction, StreamEventContextCompactionStart,
		StreamEventContextCompactionDone, StreamEventContextCompactionError, StreamEventUnknown,
	} {
		metrics.ObserveStreamEvent(metricsEvent(kind, nil, nil, nil))
	}

	snapshot := metrics.Snapshot()
	assert.Equal(t, 21, snapshot.InputTokens)
	assert.Equal(t, 8, snapshot.OutputTokens)
}

func TestRunMetricsRejectsMalformedToolTraces(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		events []StreamEvent
	}{
		{name: "nil start", events: []StreamEvent{metricsEvent(StreamEventToolStart, nil, nil, nil)}},
		{name: "empty start ID", events: []StreamEvent{metricsStart("", "", 0)}},
		{name: "duplicate start", events: []StreamEvent{
			metricsStart(metricsMalformedCall, "", 0), metricsStart(metricsMalformedCall, "", 0),
		}},
		{name: "nil result", events: []StreamEvent{metricsEvent(StreamEventToolResult, nil, nil, nil)}},
		{name: "empty result ID", events: []StreamEvent{metricsResult("", "", 0)}},
		{name: "result without start", events: []StreamEvent{metricsResult(metricsMalformedCall, "", 0)}},
		{name: "duplicate result", events: []StreamEvent{
			metricsStart(metricsMalformedCall, "", 0),
			metricsResult(metricsMalformedCall, "", 0),
			metricsResult(metricsMalformedCall, "", 0),
		}},
		{name: "identity mismatch", events: []StreamEvent{
			metricsStart(metricsMalformedCall, "parent", 1),
			metricsResult(metricsMalformedCall, "other", 2),
		}},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			metrics := new(RunMetrics)
			for _, event := range testCase.events {
				metrics.ObserveStreamEvent(event)
			}

			assert.False(t, metrics.Snapshot().TraceComplete)
		})
	}
}

func metricsTokenUsage(input, output int) model.TokenUsage {
	return model.TokenUsage{
		Breakdown: nil, TopContributors: nil, ContextWindow: 0, ContextTokens: 0,
		InputTokens: input, OutputTokens: output,
	}
}

func metricsEvent(
	kind StreamEventKind,
	call *ToolCallEvent,
	result *ToolEvent,
	usage *model.TokenUsage,
) StreamEvent {
	return StreamEvent{ToolCallEvent: call, ToolEvent: result, Usage: usage, Kind: kind, Text: ""}
}

func metricsStart(id, parent string, sequence int) StreamEvent {
	return metricsEvent(StreamEventToolStart, &ToolCallEvent{
		ArgumentsJSON: "", ID: id, ParentCallID: parent, Name: "",
		Arguments: tool.EmptyArguments(), Sequence: sequence,
	}, nil, nil)
}

func metricsResult(id, parent string, sequence int) StreamEvent {
	return metricsEvent(StreamEventToolResult, nil, &ToolEvent{
		CallID: id, ParentCallID: parent, Name: "", ArgumentsJSON: "", DetailsJSON: "",
		Result: "", Error: "", Sequence: sequence, IsError: false,
	}, nil)
}

func TestRunMetricsConcurrentProviderObservations(t *testing.T) {
	t.Parallel()

	const observations = 32

	metrics := new(RunMetrics)
	ctx := WithRunMetrics(t.Context(), metrics)

	var wait sync.WaitGroup

	wait.Add(observations)

	for range observations {
		go func() {
			defer wait.Done()

			observeProviderRoundTrip(ctx)
		}()
	}

	wait.Wait()

	assert.Equal(t, observations, metrics.ProviderRoundTrips())
}
