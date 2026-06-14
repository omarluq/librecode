package assistant

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/event"
	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/model"
)

const (
	providerHookOriginalKey   = "original"
	providerHookOriginalValue = "value"
	providerHookTestModelID   = "test-model"
	providerHookMessagesKey   = "messages"
)

func TestRuntime_ProviderRequestHookMutatesPayloadAndHeaders(t *testing.T) {
	t.Parallel()

	runtime := newProviderHookTestRuntime(t, `
local lc = require("librecode")
lc.on("before_provider_request", function(event)
  event.payload.payload.metadata = "from-extension"
  return {
    payload = event.payload,
    provider_request = {
      headers = {
        ["X-Test-Header"] = "yes",
      },
    },
  }
end)
`)
	input := providerHookTestInput(
		map[string]any{providerHookMessagesKey: []any{}},
		map[string]string{"Authorization": "Bearer secret"},
		2,
	)

	result, err := runtime.dispatchProviderRequestHook(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, "from-extension", result.Payload["metadata"])
	assert.Equal(t, "yes", result.Headers["X-Test-Header"])
	assert.Equal(t, "Bearer secret", result.Headers["Authorization"])
}

func TestRuntime_ProviderRequestHookFallsBackWhenPayloadMutationIsMissing(t *testing.T) {
	t.Parallel()

	runtime := newProviderHookTestRuntime(t, `
local lc = require("librecode")
lc.on("before_provider_request", function()
  return {
    provider_request = {
      headers = {
        ["X-Test-Header"] = "yes",
      },
    },
  }
end)
`)
	input := providerHookTestInput(
		map[string]any{providerHookOriginalKey: providerHookOriginalValue},
		map[string]string{},
		1,
	)

	result, err := runtime.dispatchProviderRequestHook(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, providerHookOriginalValue, result.Payload[providerHookOriginalKey])
	assert.Equal(t, "yes", result.Headers["X-Test-Header"])
}

func TestRuntime_ProviderRequestHookRedactsAndRejectsSensitiveHeaders(t *testing.T) {
	t.Parallel()

	manager := extension.NewManager(testProviderHookLogger())
	t.Cleanup(manager.Shutdown)
	loadProviderHookTestExtension(t, manager, `
local lc = require("librecode")
local seen = ""
lc.on("before_provider_request", function(event)
  seen = ""
  for key, value in pairs(event.payload.headers) do
    seen = seen .. key .. "=" .. tostring(value)
  end
  return {
    provider_request = {
      headers = {
        Authorization = "Bearer nope",
      },
    },
  }
end)
lc.register_command("seen_auth", "seen_auth", function()
  return seen
end)
`)

	runtime := newRuntimeFromDeps(func(deps *runtimeDeps) {
		deps.Extensions = manager
		deps.Logger = testProviderHookLogger()
	})

	input := providerHookTestInput(map[string]any{}, map[string]string{"authorization": "Bearer secret"}, 1)

	_, err := runtime.dispatchProviderRequestHook(context.Background(), input)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "sensitive header")
	seen, err := manager.ExecuteCommand(context.Background(), "seen_auth", "")
	require.NoError(t, err)
	assert.Contains(t, seen, "[redacted]")
}

func newProviderHookTestRuntime(t *testing.T, source string) *Runtime {
	t.Helper()

	manager := extension.NewManager(testProviderHookLogger())
	t.Cleanup(manager.Shutdown)
	loadProviderHookTestExtension(t, manager, source)

	return newRuntimeFromDeps(func(deps *runtimeDeps) {
		deps.Extensions = manager
		deps.Events = event.NewBus(testProviderHookLogger())
		deps.Logger = testProviderHookLogger()
	})
}

func loadProviderHookTestExtension(t *testing.T, manager *extension.Manager, source string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "provider_hook.lua")
	require.NoError(t, os.WriteFile(path, []byte(source), 0o600))
	require.NoError(t, manager.LoadFile(context.Background(), path))
}

func providerHookTestRequest() *CompletionRequest {
	return &CompletionRequest{
		OnEvent:           nil,
		OnProviderObserve: nil,
		OnProviderRequest: nil,
		ToolRegistry:      nil,
		ExecuteTools:      nil,
		SessionID:         "session-1",
		SystemPrompt:      "",
		ThinkingLevel:     "off",
		CWD:               "/work",
		Auth:              model.RequestAuth{Headers: map[string]string{}, APIKey: "", Error: "", OK: true},
		Messages:          nil,
		Usage:             model.EmptyTokenUsage(),
		Model: model.Model{
			ThinkingLevelMap: nil,
			Headers:          nil,
			Compat:           nil,
			Provider:         "test-provider",
			ID:               providerHookTestModelID,
			Name:             providerHookTestModelID,
			API:              "openai-completions",
			BaseURL:          "",
			Input:            nil,
			Cost:             model.Cost{Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0},
			ContextWindow:    0,
			MaxTokens:        0,
			Reasoning:        false,
		},
		ProviderAttempt: 1,
		DisableTools:    false,
	}
}

func testProviderHookLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
