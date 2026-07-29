package provider

import (
	"math"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/llm"
)

func TestProviderRetryAfterIsBounded(t *testing.T) {
	t.Parallel()

	const retryAfterHeader = "Retry-After"

	now := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		header http.Header
		name   string
		want   time.Duration
	}{
		{
			name:   "milliseconds overflow",
			header: http.Header{"Retry-After-Ms": []string{strconv.FormatInt(math.MaxInt64, 10)}},
			want:   MaxRetryAfter,
		},
		{
			name:   "seconds overflow",
			header: http.Header{retryAfterHeader: []string{strconv.FormatInt(math.MaxInt64, 10)}},
			want:   MaxRetryAfter,
		},
		{
			name:   "date beyond maximum",
			header: http.Header{retryAfterHeader: []string{now.Add(time.Hour).Format(http.TimeFormat)}},
			want:   MaxRetryAfter,
		},
		{
			name:   "short delay",
			header: http.Header{retryAfterHeader: []string{"3"}},
			want:   3 * time.Second,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.want, providerRetryAfter(test.header, now))
		})
	}
}

func TestReadProviderBodyRejectsBodiesAboveLimit(t *testing.T) {
	t.Parallel()

	content, err := readProviderBody(strings.NewReader(strings.Repeat("a", int(providerResponseLimitBytes)+1)))

	require.Error(t, err)
	assert.Nil(t, content)
	assert.Contains(t, err.Error(), "provider response exceeds limit")
}

func TestCodexHeadersUseStoredAccountID(t *testing.T) {
	t.Parallel()

	request := testCompletionRequestAuth("openai-codex", "access-token")
	request.Request.Auth.Headers = map[string]string{codexAccountIDHeader: "acct_123"}

	headers := codexHeaders(request)

	assert.Equal(t, "Bearer access-token", headers["Authorization"])
	assert.Equal(t, "acct_123", headers[codexAccountIDHeader])
	assert.Equal(t, codexClientHeaderValue, headers[codexOriginatorHeader])
	assert.Equal(t, codexClientHeaderValue, headers[codexUserAgentHeader])
	assert.Equal(t, codexResponsesBetaValue, headers[codexBetaHeader])
	assert.Equal(t, "text/event-stream", headers["Accept"])
}

func TestCodexHeadersPreserveExtraHeaders(t *testing.T) {
	t.Parallel()

	request := testCompletionRequestAuth("openai-codex", "access-token")
	request.Request.Auth.Headers = map[string]string{
		codexAccountIDHeader: "acct_123",
		"x-extra":            "value",
	}

	headers := codexHeaders(request)

	assert.Equal(t, "value", headers["x-extra"])
}

func TestCodexHeadersHandlesNilAuthHeaders(t *testing.T) {
	t.Parallel()

	request := testCompletionRequestAuth("openai-codex", "access-token")
	request.Request.Auth = llm.Auth{APIKey: "access-token", Headers: nil}

	headers := codexHeaders(request)

	assert.Equal(t, "Bearer access-token", headers["Authorization"])
	assert.NotContains(t, headers, codexAccountIDHeader)
}
