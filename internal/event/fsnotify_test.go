package event_test

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samber/ro"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/event"
)

func TestFileWatchStreamEmitsFileEvents(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var count atomic.Int64

	errCh := make(chan error, 1)
	subscription := event.FileWatchStream(dir).SubscribeWithContext(ctx, ro.NewObserverWithContext(
		func(_ context.Context, _ event.FileEvent) {
			count.Add(1)
			cancel()
		},
		func(_ context.Context, err error) {
			select {
			case errCh <- err:
			default:
			}
		},
		func(context.Context) {
			// Intentionally no-op: the test observes cancellation through errCh.
		},
	))
	t.Cleanup(subscription.Unsubscribe)

	watchedFile := filepath.Join(dir, "config.yaml")

	require.Eventually(t, func() bool {
		writeErr := os.WriteFile(watchedFile, []byte("updated"), 0o600)

		return writeErr == nil && count.Load() > 0
	}, 2*time.Second, 10*time.Millisecond)

	select {
	case err := <-errCh:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		require.Fail(t, "expected cancellation error")
	}
}

func TestFileWatchStreamReportsAddErrors(t *testing.T) {
	t.Parallel()

	var count atomic.Int64

	subscription := event.FileWatchStream(filepath.Join(t.TempDir(), "missing")).Subscribe(ro.NewObserver(
		func(event.FileEvent) {},
		func(error) {
			count.Add(1)
		},
		func() {},
	))
	t.Cleanup(subscription.Unsubscribe)

	require.Eventually(t, func() bool {
		return count.Load() == 1
	}, time.Second, 10*time.Millisecond)
}
