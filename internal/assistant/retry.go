package assistant

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/samber/oops"
	retrylib "github.com/sethvargo/go-retry"

	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/provider"
)

const (
	defaultRetryMaxAttempts = 3
	defaultRetryBaseDelay   = 2 * time.Second
	defaultRetryMaxDelay    = 30 * time.Second
)

type retryFailedError struct {
	err error
}

func (err *retryFailedError) Error() string {
	return err.err.Error()
}

func (err *retryFailedError) Unwrap() error {
	return err.err
}

func retryableProviderError(err error) error {
	return oops.In("assistant").Code("retryable_provider_error").Wrapf(
		retrylib.RetryableError(&retryFailedError{err: err}),
		"retry provider error",
	)
}

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

func retryBackoff(retry config.RetryConfig, onDelay func(time.Duration)) retrylib.BackoffFunc {
	retry = retry.Normalized()

	backoff := retrylib.NewExponential(retry.BaseDelay)
	capped := retrylib.WithCappedDuration(retry.MaxDelay, backoff)
	limited := retrylib.WithMaxRetries(maxRetryDelays(retry), capped)

	return retrylib.BackoffFunc(func() (time.Duration, bool) {
		delay, stop := limited.Next()
		if stop {
			return 0, true
		}

		onDelay(delay)

		return delay, false
	})
}

func maxRetryDelays(retry config.RetryConfig) uint64 {
	if retry.MaxAttempts <= 1 {
		return 0
	}

	return uint64(retry.MaxAttempts - 1)
}

// ShouldRetryModelError reports whether a model/provider error is transient.
func ShouldRetryModelError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}

	message := normalizedErrorMessage(err)
	if nonRetryableProviderMessage(message) {
		return false
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return retryableDeadlineExceeded(message)
	}

	if retry, ok := retryDecisionFromProviderCode(err); ok {
		return retry
	}

	if status, ok := providerErrorStatus(err); ok {
		return retryableStatus(status)
	}

	if retryableNetworkError(err) {
		return true
	}

	return retryableProviderMessage(message)
}

// IsContextWindowError reports whether err indicates provider-side context exhaustion.
func IsContextWindowError(err error) bool {
	if err == nil {
		return false
	}

	code, matched := providerErrorCode(err)
	if matched && code == "context_window_exceeded" {
		return true
	}

	return contextWindowProviderMessage(normalizedErrorMessage(err))
}

func retryDecisionFromProviderCode(err error) (retry, known bool) {
	code, matched := providerErrorCode(err)
	if !matched {
		return false, false
	}

	if nonRetryableProviderCode(code) {
		return false, true
	}

	if retryableProviderCode(code) {
		return true, true
	}

	return false, false
}

func retryableNetworkError(err error) bool {
	netErr, ok := errors.AsType[net.Error](err)

	return ok && netErr != nil
}

func retryableDeadlineExceeded(message string) bool {
	// Match provider/client timeout details, not wrapper call-site labels such as
	// "request provider response", so caller-owned deadlines remain non-retryable.
	providerTimeout := strings.Contains(message, "client.timeout exceeded") ||
		strings.Contains(message, "awaiting headers")
	if !providerTimeout {
		return false
	}

	return !nonRetryableProviderMessage(message)
}

func providerErrorCode(err error) (string, bool) {
	oopsErr, matched := oops.AsOops(err)
	if !matched {
		return "", false
	}

	codeValue, matched := oopsErr.Code().(string)
	if !matched {
		return "", false
	}

	code := strings.ToLower(strings.TrimSpace(codeValue))

	return code, code != ""
}

func providerErrorStatus(err error) (int, bool) {
	var statusErr *provider.StatusError
	if errors.As(err, &statusErr) {
		return statusErr.Status, true
	}

	oopsErr, matched := oops.AsOops(err)
	if !matched {
		return 0, false
	}

	status, matched := oopsErr.Context()["status"]
	if !matched {
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
	case "responses_stream_incomplete",
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
	case "context_window_exceeded",
		"openai_chat_decode",
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
	if contextWindowProviderMessage(message) {
		return true
	}

	nonRetryable := []string{
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

func contextWindowProviderMessage(message string) bool {
	negativePatterns := []string{
		"billing",
		"daily token limit",
		"monthly token limit",
		"plan limit",
		"quota exceeded",
	}
	for _, pattern := range negativePatterns {
		if strings.Contains(message, pattern) {
			return false
		}
	}

	patterns := []string{
		"context length",
		"context token limit",
		"context window",
		"input exceeds the context",
		"maximum context",
		"token limit exceeded for request",
		"token limit for this request",
		"too many tokens in request",
	}
	for _, pattern := range patterns {
		if strings.Contains(message, pattern) {
			return true
		}
	}

	return false
}

func normalizedErrorMessage(err error) string {
	if err == nil {
		return ""
	}

	return strings.ToLower(err.Error())
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
		"internal_error",
		"stream error",
		"stream id",
		"stream closed before completion",
		"received from peer",
		"http2",
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
	}
	for _, pattern := range retryable {
		if strings.Contains(message, pattern) {
			return true
		}
	}

	return false
}
