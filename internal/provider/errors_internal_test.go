package provider

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderStatusErrorUsesStructuredMessage(t *testing.T) {
	t.Parallel()

	err := providerStatusError(429, []byte(`{"error":{"message":"rate limited"}}`), nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limited")

	var statusErr *StatusError
	require.ErrorAs(t, err, &statusErr)
	assert.Equal(t, 429, statusErr.Status)
	assert.Equal(t, "rate limited", statusErr.Details.Message)

	var coded oops.OopsError
	require.ErrorAs(t, err, &coded)
	assert.Equal(t, "provider_status", coded.Code())
	assert.Equal(t, "provider", coded.Domain())
}

func TestProviderStatusErrorFallsBackToHTTPStatus(t *testing.T) {
	t.Parallel()

	err := providerStatusError(503, []byte(`{"error":{}}`), nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider returned HTTP 503")
}

func TestProviderStatusErrorIncludesStructuredDetails(t *testing.T) {
	t.Parallel()

	err := providerStatusError(400, []byte(`{
		"error": {
			"message": "unknown parameter",
			"type": "invalid_request_error",
			"code": "unknown_parameter",
			"param": "input[2].content"
		}
	}`), &RequestShape{
		Keys:                    []string{jsonInputKey},
		ByteSize:                0,
		FunctionCallCount:       0,
		FunctionCallOutputCount: 0,
		InputCount:              3,
		KeyCount:                1,
		MessageCount:            0,
		ToolCount:               0,
		HasInclude:              false,
		HasParallelToolCalls:    false,
		HasPromptCacheKey:       false,
		HasReasoning:            false,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown parameter")

	var coded oops.OopsError
	require.ErrorAs(t, err, &coded)
	context := coded.Context()
	assert.Equal(t, 400, context["status"])
	assert.Equal(t, "invalid_request_error", context[providerTypeContextKey])
	assert.Equal(t, "unknown_parameter", context[providerCodeContextKey])
	assert.Equal(t, "input[2].content", context[providerParamContextKey])
	assert.Equal(t, map[string]any{
		"has_include":             false,
		"has_parallel_tool_calls": false,
		"has_prompt_cache_key":    false,
		"has_reasoning":           false,
		"input_count":             3,
		"key_count":               1,
		"keys":                    []string{jsonInputKey},
	}, context[providerRequestShapeContextKey])
	assert.Contains(t, context[providerBodyPreviewContextKey], "unknown parameter")
}

func TestProviderStatusErrorBoundsBodyPreview(t *testing.T) {
	t.Parallel()

	tests := []struct {
		body string
		name string
	}{
		{name: "ascii", body: strings.Repeat("x", providerBodyPreviewBytes+1)},
		{name: "utf8 boundary", body: strings.Repeat("x", providerBodyPreviewBytes-1) + "☃"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := providerStatusError(400, []byte(test.body), nil)

			var coded oops.OopsError
			require.ErrorAs(t, err, &coded)
			context := coded.Context()
			preview, ok := context[providerBodyPreviewContextKey].(string)
			require.True(t, ok)
			assert.LessOrEqual(t, len(preview), providerBodyPreviewBytes)
			assert.True(t, utf8.ValidString(preview))
			assert.Equal(t, true, context[providerBodyTruncatedContextKey])
		})
	}
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
	assert.Equal(t, "top-level", errorMessageFromMap(map[string]any{jsonMessageType: "top-level"}))
	assert.Equal(t, "nested", errorMessageFromMap(map[string]any{
		anthropicErrorEvent: map[string]any{jsonMessageType: "nested"},
	}))
	assert.Equal(t, "details", errorMessageFromMap(map[string]any{"detail": "details"}))
	assert.Equal(t, "description", errorMessageFromMap(map[string]any{"error_description": "description"}))
	assert.Equal(t, "listed", errorMessageFromMap(map[string]any{
		"errors": []any{map[string]any{jsonMessageType: "listed"}},
	}))
	assert.Equal(t, "string error", errorMessageFromBytes([]byte(`"string error"`)))
}

func TestStringHelpers(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "hello", stringValue(testStringer("hello")))
	assert.Empty(t, stringValue(123))
	assert.Equal(t, "second", firstNonEmptyString(" ", "second"))
	assert.Empty(t, firstNonEmptyString(" ", ""))
}

type testStringer string

func (value testStringer) String() string { return string(value) }
