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
