package assistant

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/config"
)

func TestRetryDelayIsCapped(t *testing.T) {
	t.Parallel()

	retry := config.RetryConfig{
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    15 * time.Millisecond,
		MaxAttempts: 3,
		Enabled:     true,
	}

	delay := retryDelay(3, retry)

	assert.LessOrEqual(t, delay, retry.MaxDelay)
}

func TestWaitForRetryRespectsCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := waitForRetry(ctx, time.Hour)

	require.ErrorIs(t, err, context.Canceled)
}
