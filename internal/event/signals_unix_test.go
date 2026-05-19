//go:build !windows

package event_test

import (
	"context"
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/samber/ro"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/event"
)

func TestSignalStreamEmitsSignals(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var count atomic.Int64
	subscription := event.SignalStream(syscall.SIGUSR1).SubscribeWithContext(ctx, ro.NewObserverWithContext(
		func(_ context.Context, _ os.Signal) {
			count.Add(1)
			cancel()
		},
		func(_ context.Context, err error) {
			require.NoError(t, err)
		},
		func(context.Context) {
			// Intentionally no-op: signal stream completion is not asserted here.
		},
	))
	t.Cleanup(subscription.Unsubscribe)

	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGUSR1))
	require.Eventually(t, func() bool {
		return count.Load() == 1
	}, time.Second, 10*time.Millisecond)
}

func TestSignalContextCancelsOnSignal(t *testing.T) {
	t.Parallel()

	ctx, stop := event.SignalContext(context.Background(), syscall.SIGUSR2)
	defer stop()

	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGUSR2))
	require.Eventually(t, func() bool {
		return ctx.Err() != nil
	}, time.Second, 10*time.Millisecond)
}
