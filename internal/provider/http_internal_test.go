package provider

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/llm"
)

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
	request.Request.Auth.Headers = map[string]string{"chatgpt-account-id": "acct_123"}

	headers := codexHeaders(request)

	assert.Equal(t, "Bearer access-token", headers["Authorization"])
	assert.Equal(t, "acct_123", headers["chatgpt-account-id"])
	assert.Equal(t, "librecode", headers["originator"])
	assert.Equal(t, "librecode", headers["User-Agent"])
	assert.Equal(t, "responses=experimental", headers["OpenAI-Beta"])
	assert.Equal(t, "text/event-stream", headers["Accept"])
}

func TestCodexHeadersPreserveExtraHeaders(t *testing.T) {
	t.Parallel()

	request := testCompletionRequestAuth("openai-codex", "access-token")
	request.Request.Auth.Headers = map[string]string{
		"chatgpt-account-id": "acct_123",
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
	assert.NotContains(t, headers, "chatgpt-account-id")
}
