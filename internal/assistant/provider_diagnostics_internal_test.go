package assistant

import (
	"context"
	"testing"

	"github.com/samber/ro"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant/lifecyclepayload"
	"github.com/omarluq/librecode/internal/event"
	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/llm"
)

type providerHookDiagnosticsTestCase struct {
	assertFn func(t *testing.T, events []map[string]any, output llm.HookOutput, err error)
	input    *llm.HookInput
	lua      string
	name     string
}

func TestRuntime_ProviderRequestHookEmitsDiagnostics(t *testing.T) {
	t.Parallel()

	tests := []providerHookDiagnosticsTestCase{
		providerHookMutatedDiagnosticsCase(),
		providerHookNoopDiagnosticsCase(),
		providerHookErrorDiagnosticsCase(),
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			runProviderHookDiagnosticsTest(t, &testCase)
		})
	}
}

func providerHookMutatedDiagnosticsCase() providerHookDiagnosticsTestCase {
	return providerHookDiagnosticsTestCase{
		name: "mutated request",
		lua: `
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
`,
		input:    providerHookTestInput(map[string]any{providerHookMessagesKey: []any{}}, map[string]string{}, 3),
		assertFn: assertProviderHookMutatedDiagnostics,
	}
}

func providerHookNoopDiagnosticsCase() providerHookDiagnosticsTestCase {
	return providerHookDiagnosticsTestCase{
		name: "noop handler",
		lua: `
local lc = require("librecode")
lc.on("before_provider_request", function()
  return { provider_request = {} }
end)
`,
		input: providerHookTestInput(
			map[string]any{providerHookOriginalKey: providerHookOriginalValue},
			map[string]string{},
			1,
		),
		assertFn: assertProviderHookNoopDiagnostics,
	}
}

func providerHookErrorDiagnosticsCase() providerHookDiagnosticsTestCase {
	return providerHookDiagnosticsTestCase{
		name: "handler error",
		lua: `
local lc = require("librecode")
lc.on("before_provider_request", function()
  error("provider hook failed")
end)
`,
		input: providerHookTestInput(
			map[string]any{providerHookOriginalKey: providerHookOriginalValue},
			map[string]string{},
			1,
		),
		assertFn: assertProviderHookErrorDiagnostics,
	}
}

func assertProviderHookMutatedDiagnostics(
	t *testing.T,
	events []map[string]any,
	_ llm.HookOutput,
	err error,
) {
	t.Helper()

	require.NoError(t, err)
	require.Len(t, events, 1)
	diagnostic := events[0]
	assert.EqualValues(t, extension.LifecycleBeforeProviderRequest, diagnostic["event"])
	assert.Equal(t, 1, diagnostic[lifecyclepayload.HookCountKey])
	assert.Equal(t, 3, diagnostic[lifecyclepayload.AttemptKey])
	assert.Equal(t, providerHookTestModelID, diagnostic[lifecyclepayload.ModelKey])
	assert.Equal(t, 1, diagnostic["mutated_header_count"])
	assert.Equal(t, 2, diagnostic["mutated_payload_key_count"])
}

func assertProviderHookNoopDiagnostics(
	t *testing.T,
	events []map[string]any,
	output llm.HookOutput,
	err error,
) {
	t.Helper()

	require.NoError(t, err)
	assert.Equal(t, providerHookOriginalValue, output.Payload[providerHookOriginalKey])
	require.Len(t, events, 1)
	assert.Equal(t, 1, events[0][lifecyclepayload.HookCountKey])
}

func assertProviderHookErrorDiagnostics(
	t *testing.T,
	events []map[string]any,
	_ llm.HookOutput,
	err error,
) {
	t.Helper()

	require.Error(t, err)
	require.Len(t, events, 1)
	diagnostic := events[0]
	assert.Equal(t, 1, diagnostic[lifecyclepayload.HookCountKey])
	require.Contains(t, diagnostic, lifecyclepayload.ErrorsKey)
	hookErrors, ok := diagnostic[lifecyclepayload.ErrorsKey].([]string)
	require.True(t, ok, "hook_errors should be a string slice")
	require.NotEmpty(t, hookErrors)
	assert.Contains(t, hookErrors[0], "provider hook failed")
}

func runProviderHookDiagnosticsTest(t *testing.T, testCase *providerHookDiagnosticsTestCase) {
	t.Helper()

	runtime := newProviderHookTestRuntime(t, testCase.lua)
	events := collectAssistantDiagnostics(
		t,
		runtime.EventBus(),
		string(extension.LifecycleBeforeProviderRequest)+"_diagnostic",
	)

	output, err := runtime.dispatchProviderRequestHook(context.Background(), testCase.input)

	testCase.assertFn(t, *events, output, err)
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
