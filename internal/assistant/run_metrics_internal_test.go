package assistant

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/tool"
)

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
	observeProviderUsage(ctx, model.TokenUsage{
		Breakdown: nil, TopContributors: nil, ContextWindow: 0, ContextTokens: 0,
		InputTokens: 13, OutputTokens: 5,
	})

	metrics.ObserveStreamEvent(StreamEvent{
		ToolCallEvent: &ToolCallEvent{
			ArgumentsJSON: `{}`, ID: outerCallID, ParentCallID: "", Name: "execute",
			Arguments: tool.EmptyArguments(), Sequence: 0,
		},
		ToolEvent: nil, Usage: nil, Kind: StreamEventToolStart, Text: "",
	})
	metrics.ObserveStreamEvent(StreamEvent{
		ToolCallEvent: &ToolCallEvent{
			ArgumentsJSON: `{}`, ID: nestedCallID, ParentCallID: outerCallID, Name: jsonReadToolName,
			Arguments: tool.EmptyArguments(), Sequence: 1,
		},
		ToolEvent: nil, Usage: nil, Kind: StreamEventToolStart, Text: "",
	})
	metrics.ObserveStreamEvent(StreamEvent{
		ToolCallEvent: nil,
		ToolEvent: &ToolEvent{
			CallID: nestedCallID, ParentCallID: outerCallID, Name: jsonReadToolName, ArgumentsJSON: `{}`,
			DetailsJSON: "", Result: "ok", Error: "", Sequence: 1, IsError: false,
		},
		Usage: nil, Kind: StreamEventToolResult, Text: "",
	})
	metrics.ObserveStreamEvent(StreamEvent{
		ToolCallEvent: nil,
		ToolEvent: &ToolEvent{
			CallID: outerCallID, ParentCallID: "", Name: "execute", ArgumentsJSON: `{}`,
			DetailsJSON: "", Result: "ok", Error: "", Sequence: 0, IsError: false,
		},
		Usage: nil, Kind: StreamEventToolResult, Text: "",
	})

	assert.Equal(t, RunMetricsSnapshot{
		ProviderRoundTrips: 2, InputTokens: 13, OutputTokens: 5,
		ToolCalls: 2, NestedToolCalls: 1, TraceComplete: true,
	}, metrics.Snapshot())
}

func TestRunMetricsDetectsIncompleteTrace(t *testing.T) {
	t.Parallel()

	metrics := new(RunMetrics)
	metrics.ObserveStreamEvent(StreamEvent{
		ToolCallEvent: &ToolCallEvent{
			ArgumentsJSON: `{}`, ID: "metrics-incomplete", ParentCallID: "", Name: jsonReadToolName,
			Arguments: tool.EmptyArguments(), Sequence: 0,
		},
		ToolEvent: nil, Usage: nil, Kind: StreamEventToolStart, Text: "",
	})

	assert.False(t, metrics.Snapshot().TraceComplete)
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
