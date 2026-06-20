package assistant

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/config"
)

func TestRetryBackoffUsesCappedExponentialDelays(t *testing.T) {
	t.Parallel()

	retry := config.RetryConfig{
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    15 * time.Millisecond,
		MaxAttempts: 4,
		Enabled:     true,
	}
	delays := []time.Duration{}
	backoff := retryBackoff(retry, func(delay time.Duration) {
		delays = append(delays, delay)
	})

	for range retry.MaxAttempts - 1 {
		delay, stop := backoff.Next()

		require.False(t, stop)
		assert.LessOrEqual(t, delay, retry.MaxDelay)
	}

	_, stop := backoff.Next()
	require.True(t, stop)
	assert.Equal(t, []time.Duration{10 * time.Millisecond, 15 * time.Millisecond, 15 * time.Millisecond}, delays)
}

func TestShouldRetryModelErrorTreatsHTTP2StreamErrorsAsTransient(t *testing.T) {
	t.Parallel()

	err := errors.New("read provider stream: stream error: stream ID 193; INTERNAL_ERROR; received from peer")

	assert.True(t, ShouldRetryModelError(err))
}
