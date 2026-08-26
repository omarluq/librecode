package workflowprogress_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/workflowprogress"
)

func TestEmitterOwnsSequenceStateAndTypedEnvelopes(t *testing.T) {
	t.Parallel()

	var events []workflowprogress.Event

	emitter := workflowprogress.New(func(_ context.Context, event workflowprogress.Event) error {
		events = append(events, event)

		return nil
	})

	require.NoError(t, emitter.Phase(t.Context(), "review", "Review", "running"))
	require.NoError(t, emitter.Item(t.Context(), "lint", "review", "Lint", "pending"))
	require.NoError(t, emitter.Event(t.Context(), "files", map[string]any{"count": 3}))
	require.NoError(t, emitter.Log(t.Context(), "info", "checking"))
	require.NoError(t, emitter.Item(t.Context(), "lint", "review", "Lint", "succeeded"))
	require.NoError(t, emitter.Phase(t.Context(), "review", "Review", "succeeded"))

	require.Len(t, events, 6)

	for index, event := range events {
		assert.Equal(t, workflowprogress.ContractVersion, event.Version)
		assert.Equal(t, uint64(index+1), event.Sequence)
		require.NoError(t, workflowprogress.ValidateEvent(event))
	}

	assert.Equal(t, workflowprogress.KindPhase, events[0].Kind)
	assert.Equal(t, workflowprogress.KindItem, events[1].Kind)
	assert.Equal(t, workflowprogress.KindEvent, events[2].Kind)
	assert.Equal(t, workflowprogress.KindLog, events[3].Kind)
}

func TestEmitterRejectsInvalidAndMutableTerminalProgress(t *testing.T) {
	t.Parallel()

	emitter := workflowprogress.New(nil)
	require.ErrorContains(t, emitter.Item(t.Context(), "item", "missing", "Item", "running"), "not declared")
	require.ErrorContains(t, emitter.Phase(t.Context(), "phase", "Phase", "unknown"), "invalid progress state")
	require.ErrorContains(t, emitter.Log(
		t.Context(), "info", strings.Repeat("x", workflowprogress.MaxTextBytes+1),
	), "limit")
	require.ErrorContains(t, emitter.Event(t.Context(), "large", map[string]any{
		"value": strings.Repeat("x", workflowprogress.MaxDataBytes),
	}), "limit")

	require.NoError(t, emitter.Phase(t.Context(), "phase", "Phase", "succeeded"))
	require.ErrorContains(t, emitter.Phase(t.Context(), "phase", "changed", "running"), "is terminal")
}

func TestEmitterBoundsEventCountWithoutAdvancingRejectedEvents(t *testing.T) {
	t.Parallel()

	emitter := workflowprogress.New(nil)
	for range workflowprogress.MaxEvents {
		require.NoError(t, emitter.Log(t.Context(), "debug", "tick"))
	}

	require.ErrorContains(t, emitter.Log(t.Context(), "debug", "overflow"), "limit")
}
