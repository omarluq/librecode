package assistant

import (
	"context"
	"testing"

	"github.com/samber/ro"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/event"
	"github.com/omarluq/librecode/internal/extension"
)

func TestRuntime_ProviderRequestHookEmitsDiagnostics(t *testing.T) {
	t.Parallel()

	runtime := newProviderHookTestRuntime(t, `
local lc = require("librecode")
lc.on("before_provider_request", { priority = 10 }, function(event)
  event.payload.payload.metadata = "changed"
  return {
    payload = event.payload,
    provider_request = {
      headers = {
        ["X-Debug"] = "yes",
      },
    },
  }
end)
`)
	events := collectAssistantDiagnostics(
		t,
		runtime.EventBus(),
		string(extension.LifecycleBeforeProviderRequest)+"_diagnostic",
	)
	input := providerHookInput{
		Request: providerHookTestRequest(),
		Payload: map[string]any{providerHookMessagesKey: []any{}},
		Headers: map[string]string{},
		Attempt: 3,
	}

	_, err := runtime.dispatchProviderRequestHook(context.Background(), input)

	require.NoError(t, err)
	require.Len(t, *events, 1)
	diagnostic := (*events)[0]
	assert.Equal(t, string(extension.LifecycleBeforeProviderRequest), diagnostic["event"])
	assert.Equal(t, 1, diagnostic[lifecycleHookCountKey])
	assert.Equal(t, 3, diagnostic[lifecycleAttemptKey])
	assert.Equal(t, providerHookTestModelID, diagnostic[jsonModelKey])
	assert.Equal(t, 1, diagnostic["mutated_header_count"])
	assert.Equal(t, 2, diagnostic["mutated_payload_key_count"])
}

func TestRuntime_ProviderRequestHookEmitsDiagnosticsForNoopHandler(t *testing.T) {
	t.Parallel()

	runtime := newProviderHookTestRuntime(t, `
local lc = require("librecode")
lc.on("before_provider_request", function()
  return { provider_request = {} }
end)
`)
	events := collectAssistantDiagnostics(
		t,
		runtime.EventBus(),
		string(extension.LifecycleBeforeProviderRequest)+"_diagnostic",
	)
	input := providerHookInput{
		Request: providerHookTestRequest(),
		Payload: map[string]any{providerHookOriginalKey: providerHookOriginalValue},
		Headers: map[string]string{},
		Attempt: 1,
	}

	result, err := runtime.dispatchProviderRequestHook(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, providerHookOriginalValue, result.Payload[providerHookOriginalKey])
	require.Len(t, *events, 1)
	assert.Equal(t, 1, (*events)[0][lifecycleHookCountKey])
}

func TestRuntime_ProviderRequestHookEmitsDiagnosticsOnHandlerError(t *testing.T) {
	t.Parallel()

	runtime := newProviderHookTestRuntime(t, `
local lc = require("librecode")
lc.on("before_provider_request", function()
  error("provider hook failed")
end)
`)
	events := collectAssistantDiagnostics(
		t,
		runtime.EventBus(),
		string(extension.LifecycleBeforeProviderRequest)+"_diagnostic",
	)
	input := providerHookInput{
		Request: providerHookTestRequest(),
		Payload: map[string]any{providerHookOriginalKey: providerHookOriginalValue},
		Headers: map[string]string{},
		Attempt: 1,
	}

	_, err := runtime.dispatchProviderRequestHook(context.Background(), input)

	require.Error(t, err)
	require.Len(t, *events, 1)
	diagnostic := (*events)[0]
	assert.Equal(t, 1, diagnostic[lifecycleHookCountKey])
	require.Contains(t, diagnostic, lifecycleErrorsKey)
}

func collectAssistantDiagnostics(t *testing.T, bus *event.Bus, channel string) *[]map[string]any {
	t.Helper()

	diagnostics := []map[string]any{}
	subscription := bus.Channel(channel).Subscribe(ro.NewObserver(
		func(envelope event.Envelope) {
			payload, ok := envelope.Data.(map[string]any)
			if ok {
				diagnostics = append(diagnostics, payload)
			}
		},
		func(error) {},
		func() {},
	))
	t.Cleanup(subscription.Unsubscribe)

	return &diagnostics
}
