package assistant_test

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/model"
)

func TestRuntime_ProviderLifecycleEmitsDiagnostics(t *testing.T) {
	t.Parallel()

	runtime, _, _ := newTestRuntimeWithManager(t, staticCompleter{
		result: &assistant.CompletionResult{
			FinishReason: llm.FinishReasonStop,
			Text:         "provider response",
			Thinking:     []string{"thinking"},
			ToolEvents: []assistant.ToolEvent{{
				Name:          "read",
				ArgumentsJSON: `{}`,
				DetailsJSON:   `{}`,
				Result:        "ok",
				Error:         "",
				IsError:       false,
			}},
			Usage: model.TokenUsage{
				Breakdown:       nil,
				TopContributors: nil,
				ContextWindow:   1000,
				ContextTokens:   100,
				InputTokens:     80,
				OutputTokens:    20,
			},
		},
		err: nil,
	})
	responseDiagnostics := collectRuntimeDiagnosticPayloads(
		t,
		runtime.EventBus(),
		string(extension.LifecycleAfterProviderResponse)+"_diagnostic",
	)

	_, err := runtime.Prompt(context.Background(), newRuntimePromptRequest(testRuntimeCWD, "provider diagnostics", ""))

	require.NoError(t, err)
	require.Len(t, *responseDiagnostics, 1)
	response := (*responseDiagnostics)[0]
	assert.Equal(t, 0, response["hook_count"])
	assert.Equal(t, "test-provider", response["provider"])
	assert.Equal(t, "test-model", response["model"])
	assert.Equal(t, len("provider response"), response["response_text_bytes"])
	assert.Equal(t, 1, response["thinking_count"])
	assert.Equal(t, 1, response["tool_event_count"])
	assert.Equal(t, 100, response["context_tokens"])
	assert.Equal(t, 1000, response["context_window"])
	assert.Equal(t, 80, response["input_tokens"])
	assert.Equal(t, 20, response["output_tokens"])
}

func TestRuntime_ProviderErrorDiagnosticsIncludeRetryAndErrorMetadata(t *testing.T) {
	t.Parallel()

	providerErr := oops.In("assistant").
		Code("responses_status").
		With("status", 503).
		Errorf("service unavailable")
	runtime, _, _ := newTestRuntimeWithManager(t, staticCompleter{
		result: nil,
		err:    providerErr,
	})
	errorDiagnostics := collectRuntimeDiagnosticPayloads(
		t,
		runtime.EventBus(),
		string(extension.LifecycleProviderError)+"_diagnostic",
	)

	_, err := runtime.Prompt(context.Background(), newRuntimePromptRequest(testRuntimeCWD, "provider error", ""))

	require.Error(t, err)
	require.Len(t, *errorDiagnostics, 3)
	for _, diagnostic := range *errorDiagnostics {
		assert.Equal(t, "test-provider", diagnostic["provider"])
		assert.Equal(t, "test-model", diagnostic["model"])
		assert.Equal(t, true, diagnostic["retryable"])
		assert.Equal(t, "responses_status", diagnostic["error_code"])
		assert.Equal(t, 503, diagnostic["status"])
		assert.NotEmpty(t, diagnostic["error"])
	}
}

func TestRuntime_ProviderErrorDiagnosticsCaptureHookErrors(t *testing.T) {
	t.Parallel()

	runtime, _, manager := newTestRuntimeWithManager(t, staticCompleter{
		result: nil,
		err:    errors.New("provider unavailable"),
	})
	loadRuntimeExtension(t, manager, `
local lc = require("librecode")
lc.on("provider_error", function()
  error("provider error hook failed")
end)
`)
	errorDiagnostics := collectRuntimeDiagnosticPayloads(
		t,
		runtime.EventBus(),
		string(extension.LifecycleProviderError)+"_diagnostic",
	)

	_, err := runtime.Prompt(context.Background(), newRuntimePromptRequest(testRuntimeCWD, "provider hook error", ""))

	require.Error(t, err)
	require.Len(t, *errorDiagnostics, 1)
	for _, diagnostic := range *errorDiagnostics {
		assert.Equal(t, 1, diagnostic["hook_count"])
		hookErrors, ok := diagnostic["hook_errors"].([]string)
		require.True(t, ok)
		require.NotEmpty(t, hookErrors)
		assert.Contains(t, hookErrors[0], "provider error hook failed")
	}
}
