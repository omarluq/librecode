package assistant

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/provider"
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

func TestProviderRetryDelayHonorsLongerProviderDelay(t *testing.T) {
	t.Parallel()

	err := &provider.StatusError{
		Details:      nil,
		RequestShape: nil,
		RequestID:    "",
		RetryAfter:   time.Minute,
		Status:       0,
	}

	assert.Equal(t, time.Minute, providerRetryDelay(err, 2*time.Second))
	assert.Equal(t, 2*time.Minute, providerRetryDelay(err, 2*time.Minute))

	delays := []time.Duration{}
	backoff := retryBackoffWithOverride(config.RetryConfig{
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    15 * time.Millisecond,
		MaxAttempts: 2,
		Enabled:     true,
	}, func(delay time.Duration) time.Duration {
		return providerRetryDelay(err, delay)
	}, func(delay time.Duration) {
		delays = append(delays, delay)
	})

	delay, stop := backoff.Next()
	require.False(t, stop)
	assert.Equal(t, time.Minute, delay)
	assert.Equal(t, []time.Duration{time.Minute}, delays)
}

func TestShouldRetryModelErrorTreatsHTTP2StreamErrorsAsTransient(t *testing.T) {
	t.Parallel()

	err := errors.New("read provider stream: stream error: stream ID 193; INTERNAL_ERROR; received from peer")

	assert.True(t, ShouldRetryModelError(err))
}
