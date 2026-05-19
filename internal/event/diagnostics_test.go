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
	t.Cleanup(observer.Stop)

	bus.Emit(context.Background(), "turn_start", map[string]any{"session_id": "s1"})

	require.Eventually(t, func() bool {
		return strings.Contains(output.String(), "turn_start")
	}, time.Second, 10*time.Millisecond)
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
		func(context.Context, error) {},
		func(context.Context) {},
	))
	t.Cleanup(subscription.Unsubscribe)

	require.Eventually(t, func() bool {
		return count.Load() >= 2
	}, time.Second, 10*time.Millisecond)
}
