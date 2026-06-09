package llm_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/llm"
)

func TestTextMessageCreatesTextPart(t *testing.T) {
	t.Parallel()

	message := llm.TextMessage(llm.RoleUser, "hello")

	assert.Equal(t, llm.RoleUser, message.Role)
	require.Len(t, message.Content, 1)
	assert.Equal(t, llm.PartText, message.Content[0].Type)
	assert.Equal(t, "hello", message.Content[0].Text)
	assert.Nil(t, message.Content[0].Metadata)
	assert.Nil(t, message.Content[0].ToolCall)
	assert.Nil(t, message.Content[0].ToolResult)
}

func TestUsageHelpers(t *testing.T) {
	t.Parallel()

	empty := llm.EmptyUsage()
	assert.False(t, empty.HasAny())
	assert.Zero(t, empty.TotalTokens())
	assert.Zero(t, empty.ContextPercent())

	reported := llm.Usage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   100,
		ContextTokens:   25,
		InputTokens:     10,
		OutputTokens:    3,
	}
	assert.True(t, reported.HasAny())
	assert.Equal(t, 13, reported.TotalTokens())
	assert.Equal(t, 25, reported.ContextPercent())

	overWindow := llm.Usage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   100,
		ContextTokens:   150,
		InputTokens:     0,
		OutputTokens:    0,
	}
	assert.Equal(t, 100, overWindow.ContextPercent())

	inputOnly := llm.Usage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   0,
		ContextTokens:   0,
		InputTokens:     7,
		OutputTokens:    0,
	}
	assert.True(t, inputOnly.HasAny())
	assert.Equal(t, 7, inputOnly.TotalTokens())
	assert.Zero(t, inputOnly.ContextPercent())
}

func TestProviderError(t *testing.T) {
	t.Parallel()

	cause := errors.New("wrapped")
	err := &llm.ProviderError{
		Cause:        cause,
		Metadata:     map[string]any{"request_id": "req_1"},
		Kind:         llm.ErrorKindRateLimit,
		Provider:     "openai",
		Model:        "gpt-test",
		Code:         "rate_limited",
		ProviderCode: "rate_limit_exceeded",
		Message:      "slow down",
		StatusCode:   429,
	}

	assert.Equal(t, "slow down", err.Error())
	assert.ErrorIs(t, err, cause)
	assert.True(t, llm.IsKind(err, llm.ErrorKindRateLimit))
	assert.False(t, llm.IsKind(err, llm.ErrorKindAuth))
	unwrapped, ok := llm.AsProviderError(err)
	require.True(t, ok)
	assert.Same(t, err, unwrapped)
}

func TestProviderErrorFallbackMessages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err  *llm.ProviderError
		name string
		want string
	}{
		{name: "nil", err: nil, want: ""},
		{
			name: "cause",
			err: &llm.ProviderError{
				Cause:        errors.New("transport failed"),
				Metadata:     nil,
				Kind:         llm.ErrorKindNetwork,
				Provider:     "",
				Model:        "",
				Code:         "",
				ProviderCode: "",
				Message:      "",
				StatusCode:   0,
			},
			want: "transport failed",
		},
		{
			name: "code",
			err: &llm.ProviderError{
				Cause:        nil,
				Metadata:     nil,
				Kind:         llm.ErrorKindBadRequest,
				Provider:     "",
				Model:        "",
				Code:         "bad_request",
				ProviderCode: "",
				Message:      "",
				StatusCode:   0,
			},
			want: "provider error: bad_request",
		},
		{
			name: "empty",
			err: &llm.ProviderError{
				Cause:        nil,
				Metadata:     nil,
				Kind:         llm.ErrorKindUnknown,
				Provider:     "",
				Model:        "",
				Code:         "",
				ProviderCode: "",
				Message:      "",
				StatusCode:   0,
			},
			want: "provider error",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.want, test.err.Error())
		})
	}
}

func TestProviderErrorNilHelpers(t *testing.T) {
	t.Parallel()

	var providerError *llm.ProviderError
	assert.NoError(t, providerError.Unwrap())
	assert.False(t, llm.IsKind(assert.AnError, llm.ErrorKindUnknown))
	converted, ok := llm.AsProviderError(assert.AnError)
	assert.False(t, ok)
	assert.Nil(t, converted)
}
