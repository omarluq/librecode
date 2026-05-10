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

func providerStatusTestError(status int, message string) error {
	return oops.In("assistant").
		Code("provider_status").
		With("status", status).
		Errorf("%s", message)
}
