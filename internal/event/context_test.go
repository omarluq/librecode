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

func TestAfterContextStopsOnCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	values, _, err := ro.CollectWithContext(ctx, event.AfterContext(ctx, time.Hour))

	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, values)
}
