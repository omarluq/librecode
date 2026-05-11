package assistant

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/config"
)

const (
	defaultRetryMaxAttempts = 3
	defaultRetryBaseDelay   = 2 * time.Second
	defaultRetryMaxDelay    = 30 * time.Second
)

// RetryEventKind identifies model retry lifecycle events.
type RetryEventKind string

const (
	// RetryEventStart is emitted after a retryable model error before waiting.
	RetryEventStart RetryEventKind = "retry_start"
	// RetryEventEnd is emitted after a later attempt succeeds.
	RetryEventEnd RetryEventKind = "retry_end"
)

// RetryEvent describes a model retry lifecycle transition.
type RetryEvent struct {
	Kind        RetryEventKind `json:"kind"`
	Error       string         `json:"error,omitempty"`
	Attempt     int            `json:"attempt"`
	MaxAttempts int            `json:"max_attempts"`
	Delay       time.Duration  `json:"delay,omitempty"`
}

func retryConfig(cfg *config.Config) config.RetryConfig {
	if cfg == nil {
		return defaultRetryConfig()
	}
	return cfg.Assistant.Retry.Normalized()
}

func defaultRetryConfig() config.RetryConfig {
	return config.RetryConfig{
		Enabled:     true,
		MaxAttempts: defaultRetryMaxAttempts,
		BaseDelay:   defaultRetryBaseDelay,
		MaxDelay:    defaultRetryMaxDelay,
	}
}

func retryDelay(attempt int, retry config.RetryConfig) time.Duration {
	retry = retry.Normalized()
	if attempt < 1 {
		attempt = 1
	}
	delay := retry.BaseDelay
	for range attempt - 1 {
		delay *= 2
		if delay >= retry.MaxDelay {
			return retry.MaxDelay
		}
	}
	if delay > retry.MaxDelay {
		return retry.MaxDelay
	}
	return delay
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ShouldRetryModelError reports whether a model/provider error is transient.
func ShouldRetryModelError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if code, ok := providerErrorCode(err); ok {
		if nonRetryableProviderCode(code) {
			return false
		}
		if retryableProviderCode(code) {
			return true
		}
	}
	if status, ok := providerErrorStatus(err); ok {
		return retryableStatus(status)
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	message := strings.ToLower(err.Error())
	if nonRetryableProviderMessage(message) {
		return false
	}
	return retryableProviderMessage(message)
}

func providerErrorCode(err error) (string, bool) {
	oopsErr, ok := oops.AsOops(err)
	if !ok {
		return "", false
	}
	codeValue, ok := oopsErr.Code().(string)
	if !ok {
		return "", false
	}
	code := strings.ToLower(strings.TrimSpace(codeValue))

	return code, code != ""
}

func providerErrorStatus(err error) (int, bool) {
	oopsErr, ok := oops.AsOops(err)
	if !ok {
		return 0, false
	}
	status, ok := oopsErr.Context()["status"]
	if !ok {
		return 0, false
	}
	switch value := status.(type) {
	case int:
		return value, true
	case int64:
		return int(value), true
	case float64:
		return int(value), true
	default:
		return 0, false
	}
}

func retryableProviderCode(code string) bool {
	switch code {
	case "openai_chat_empty",
		"anthropic_empty",
		"responses_empty",
		"responses_http",
		"provider_http",
		"responses_read",
		"provider_read",
		"sse_read":
		return true
	default:
		return false
	}
}

func nonRetryableProviderCode(code string) bool {
	switch code {
	case "openai_chat_decode",
		"anthropic_decode",
		"openai_response_decode",
		"provider_payload",
		"provider_request",
		"unsupported_provider_api":
		return true
	default:
		return false
	}
}

func retryableStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests,
		http.StatusRequestTimeout,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return status >= 500 && status < 600
	}
}

func nonRetryableProviderMessage(message string) bool {
	nonRetryable := []string{
		"context length",
		"context window",
		"maximum context",
		"token limit",
		"invalid api key",
		"unauthorized",
		"authentication",
		"permission denied",
		"forbidden",
		"bad request",
		"invalid request",
	}
	for _, pattern := range nonRetryable {
		if strings.Contains(message, pattern) {
			return true
		}
	}
	return false
}

func retryableProviderMessage(message string) bool {
	retryable := []string{
		"429",
		"rate limit",
		"too many requests",
		"overloaded",
		"temporarily unavailable",
		"service unavailable",
		"server error",
		"internal error",
		"bad gateway",
		"gateway timeout",
		"timeout",
		"timed out",
		"connection reset",
		"connection refused",
		"connection closed",
		"socket hang up",
		"fetch failed",
		"websocket closed",
		"websocket error",
		"terminated",
		"empty response",
		"returned an empty response",
	}
	for _, pattern := range retryable {
		if strings.Contains(message, pattern) {
			return true
		}
	}
	return false
}
