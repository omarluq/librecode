package event_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samber/ro"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/event"
)

func TestDiagnosticObserverLogsRuntimeEvents(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer

	logger := slog.New(slog.NewTextHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug}))
	bus := event.NewBus(logger)
	observer := event.NewDiagnosticObserver(bus, logger)

	bus.Emit(context.Background(), "turn_start", map[string]any{"session_id": "s1"})
	bus.Emit(context.Background(), "turn_end", nil)

	require.Eventually(t, func() bool {
		logged := output.String()

		return strings.Contains(logged, "turn_start") && strings.Contains(logged, "\u003cnil\u003e")
	}, time.Second, 10*time.Millisecond)

	observer.Stop()
	observer.Stop()

	nilObserver := event.NewDiagnosticObserver(nil, logger)
	nilObserver.Stop()

	nilLoggerObserver := event.NewDiagnosticObserver(bus, nil)
	nilLoggerObserver.Stop()
}

func TestTickStreamEmitsIntervalValues(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var count atomic.Int64

	subscription := event.TickStream(time.Millisecond).SubscribeWithContext(ctx, ro.NewObserverWithContext(
		func(_ context.Context, _ int64) {
			if count.Add(1) >= 2 {
				cancel()
			}
		},
		func(_ context.Context, err error) {
			require.NoError(t, err)
		},
		func(context.Context) {
			// Intentionally no-op: completion is not relevant to this ticker test.
		},
	))
	t.Cleanup(subscription.Unsubscribe)

	require.Eventually(t, func() bool {
		return count.Load() >= 2
	}, time.Second, 10*time.Millisecond)
}
