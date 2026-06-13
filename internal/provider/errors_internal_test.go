package provider

import (
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderStatusErrorUsesStructuredMessage(t *testing.T) {
	t.Parallel()

	err := providerStatusError("provider_status", 429, []byte(`{"error":{"message":"rate limited"}}`))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limited")

	var coded oops.OopsError
	require.ErrorAs(t, err, &coded)
	assert.Equal(t, "provider_status", coded.Code())
	assert.Equal(t, "provider", coded.Domain())
}

func TestProviderStatusErrorFallsBackToHTTPStatus(t *testing.T) {
	t.Parallel()

	err := providerStatusError("provider_status", 503, []byte(`{"error":{}}`))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider returned HTTP 503")
}

func TestProviderErrorToOopsUsesFallbackAndMetadata(t *testing.T) {
	t.Parallel()

	err := providerErrorToOops("provider_error", &providerError{
		Message: "",
		Type:    "invalid_request",
		Code:    "bad_model",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider returned an error")

	var coded oops.OopsError
	require.ErrorAs(t, err, &coded)
	assert.Equal(t, "provider_error", coded.Code())
	assert.Equal(t, "provider", coded.Domain())
	assert.Equal(t, "invalid_request", coded.Context()[jsonTypeKey])
	assert.Equal(t, "bad_model", coded.Context()["provider_code"])
}

func TestErrorMessageFromBytesFallbacks(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "plain error", errorMessageFromBytes([]byte(" plain error ")))
	assert.Equal(t, "top-level", errorMessage(map[string]any{jsonMessageType: "top-level"}))
	assert.Equal(t, "nested", errorMessage(map[string]any{
		"error": map[string]any{jsonMessageType: "nested"},
	}))
	assert.Equal(t, "string error", errorMessage("string error"))
	assert.Empty(t, errorMessage(123))
}

func TestStringHelpers(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "hello", stringValue(testStringer("hello")))
	assert.Empty(t, stringValue(123))
	assert.Equal(t, "second", firstNonEmptyString(" ", 123, testStringer("second")))
	assert.Empty(t, firstNonEmptyString(" ", 123))
}

type testStringer string

func (value testStringer) String() string { return string(value) }
