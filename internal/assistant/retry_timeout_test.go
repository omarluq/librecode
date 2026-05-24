package assistant_test

import (
	"context"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/assistant"
)

func TestShouldRetryModelErrorHandlesDeadlineExceeded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err  error
		name string
		want bool
	}{
		{
			name: "provider client timeout is transient",
			err: oops.In("assistant").
				Code("responses_http").
				Wrapf(
					context.DeadlineExceeded,
					`request provider response: Post "https://chatgpt.com/backend-api/codex/responses": `+
						`context deadline exceeded (Client.Timeout exceeded while awaiting headers)`,
				),
			want: true,
		},
		{
			name: "caller deadline is not retried",
			err:  context.DeadlineExceeded,
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.want, assistant.ShouldRetryModelError(test.err))
		})
	}
}
