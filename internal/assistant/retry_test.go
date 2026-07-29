package assistant_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/assistant"
)

func TestShouldRetryModelError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err  error
		name string
		want bool
	}{
		{
			name: "rate limited status",
			err: providerStatusTestError(
				http.StatusTooManyRequests,
				"rate limited",
			),
			want: true,
		},
		{
			name: "server status",
			err: providerStatusTestError(
				http.StatusInternalServerError,
				"server error",
			),
			want: true,
		},
		{
			name: "bad request status",
			err: providerStatusTestError(
				http.StatusBadRequest,
				"bad request",
			),
			want: false,
		},
		{
			name: "context overflow message",
			err:  errors.New("maximum context length exceeded"),
			want: false,
		},
		{
			name: "billing token limit message",
			err:  errors.New("daily token limit exceeded; upgrade your billing plan"),
			want: false,
		},
		{
			name: "provider decode code",
			err:  oops.In("assistant").Code("openai_response_decode").Errorf("decode response"),
			want: false,
		},
		{
			name: "overloaded message",
			err:  errors.New("provider is overloaded, please try again"),
			want: true,
		},
		{
			name: "explicit provider retry guidance",
			err:  errors.New("an error occurred while processing your request; you can retry your request"),
			want: true,
		},
		{
			name: "quota remains non-retryable despite retry guidance",
			err:  errors.New("quota exceeded; you can retry your request"),
			want: false,
		},
		{
			name: "canceled context",
			err:  context.Canceled,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, assistant.ShouldRetryModelError(tt.err))
		})
	}
}

func TestShouldRetryModelErrorHandlesResponsesStreamFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err  error
		name string
		want bool
	}{
		{
			name: "stream closed before completion code",
			err: oops.In("assistant").
				Code("responses_stream_incomplete").
				Errorf("provider stream closed before completion"),
			want: true,
		},
		{
			name: "response failed with transient provider code",
			err: oops.In("assistant").Code("responses_failed").
				With("provider_code", "server_error").
				Errorf("processing failed"),
			want: true,
		},
		{
			name: "response failed with transient type and unknown code",
			err: oops.In("assistant").Code("responses_failed").
				With("provider_type", "server_error").
				With("provider_code", "backend_specific").
				Errorf("processing failed"),
			want: true,
		},
		{
			name: "non-retryable type overrides transient code",
			err: oops.In("assistant").Code("responses_failed").
				With("provider_type", "invalid_request_error").
				With("provider_code", "server_error").
				Errorf("invalid request"),
			want: false,
		},
		{
			name: "response failed without retryable details",
			err:  oops.In("assistant").Code("responses_failed").Errorf("invalid prompt"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, assistant.ShouldRetryModelError(tt.err))
		})
	}
}

func providerStatusTestError(status int, message string) error {
	return oops.In("assistant").
		Code("provider_status").
		With("status", status).
		Errorf("%s", message)
}
