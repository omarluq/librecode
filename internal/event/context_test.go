package event_test

import (
	"context"
	"testing"
	"time"

	"github.com/samber/ro"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/event"
)

func TestAfterContextEmitsOnce(t *testing.T) {
	t.Parallel()

	values, err := ro.Collect(event.AfterContext(context.Background(), time.Millisecond))

	require.NoError(t, err)
	require.Len(t, values, 1)
}

func TestAfterContextEmitsImmediatelyForNonPositiveDelay(t *testing.T) {
	t.Parallel()

	values, err := ro.Collect(event.AfterContext(context.Background(), 0))

	require.NoError(t, err)
	require.Equal(t, []int64{0}, values)
}

func TestAfterContextStopsOnSourceCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	values, err := ro.Collect(event.AfterContext(ctx, time.Hour))

	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, values)
}

func TestAfterContextStopsOnSubscriberCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	values, _, err := ro.CollectWithContext(ctx, event.AfterContext(context.Background(), time.Hour))

	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, values)
}

func TestContextDoneEmitsOnCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	values, err := ro.Collect(event.ContextDone(ctx))

	require.NoError(t, err)
	require.Len(t, values, 1)
}
