package event

import (
	"context"
	"sync"
	"time"

	"github.com/samber/ro"
)

// TickStream returns a context-aware interval stream for host loops that need
// an ro-native ticker.
func TickStream(interval time.Duration) ro.Observable[int64] {
	return ro.Interval(interval)
}

// AfterContext returns a context-aware one-shot timer stream. The stream emits
// once after delay and then completes, or errors with the controlling context
// when cancellation wins the race.
func AfterContext(ctx context.Context, delay time.Duration) ro.Observable[int64] {
	return ro.NewObservableWithContext(func(subscriberCtx context.Context, destination ro.Observer[int64]) ro.Teardown {
		if delay <= 0 {
			destination.NextWithContext(subscriberCtx, 0)
			destination.CompleteWithContext(subscriberCtx)

			return nil
		}

		timer := time.NewTimer(delay)
		done := make(chan struct{})

		var closeDone sync.Once

		go func() {
			defer timer.Stop()

			select {
			case <-timer.C:
				destination.NextWithContext(subscriberCtx, 0)
				destination.CompleteWithContext(subscriberCtx)
			case <-ctx.Done():
				destination.ErrorWithContext(subscriberCtx, ctx.Err())
			case <-subscriberCtx.Done():
				destination.ErrorWithContext(subscriberCtx, subscriberCtx.Err())
			case <-done:
				destination.CompleteWithContext(subscriberCtx)
			}
		}()

		return func() {
			closeDone.Do(func() {
				close(done)
			})
		}
	})
}

// ContextDone converts context cancellation into an ro observable. It is useful
// as a boundary signal for TakeUntil in runtime pipelines.
func ContextDone(ctx context.Context) ro.Observable[struct{}] {
	return ro.NewObservableWithContext(func(_ context.Context, destination ro.Observer[struct{}]) ro.Teardown {
		done := make(chan struct{})

		var closeDone sync.Once

		go func() {
			select {
			case <-ctx.Done():
				destination.Next(struct{}{})
				destination.Complete()
			case <-done:
				destination.Complete()
			}
		}()

		return func() {
			closeDone.Do(func() {
				close(done)
			})
		}
	})
}
