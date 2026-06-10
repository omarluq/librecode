package assistant_test

import (
	"context"
	"testing"

	"github.com/samber/ro"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/event"
	"github.com/omarluq/librecode/internal/extension"
)

func TestRuntime_ToolLifecycleEmitsDiagnostics(t *testing.T) {
	t.Parallel()

	runtime, _, manager := newTestRuntimeWithManager(t, testCompleter{})
	loadRuntimeExtension(t, manager, `
local lc = require("librecode")
lc.on("tool_call", function(event)
  return {
    tool_call = {
      arguments = {
        path = "changed.txt",
      },
    },
  }
end)
lc.on("tool_result", function(event)
  return {
    tool_result = {
      result = "redacted",
    },
  }
end)
`)
	toolCallDiagnostics := collectRuntimeDiagnosticPayloads(
		t,
		runtime.EventBus(),
		string(extension.LifecycleToolCall)+"_diagnostic",
	)
	toolResultDiagnostics := collectRuntimeDiagnosticPayloads(
		t,
		runtime.EventBus(),
		string(extension.LifecycleToolResult)+"_diagnostic",
	)
	call := assistant.ToolCallEvent{
		Arguments:     map[string]any{testToolPathKey: testToolPath},
		ID:            testToolCallID,
		Name:          testToolName,
		ArgumentsJSON: testToolArgsJSON,
	}
	toolEvent := &assistant.ToolEvent{
		Name:          testToolName,
		ArgumentsJSON: testToolArgsJSON,
		DetailsJSON:   "{}",
		Result:        "secret",
		Error:         "",
		IsError:       false,
	}

	require.NoError(t, runtime.DispatchToolCallLifecycleForTest(context.Background(), &call))
	require.NoError(t, runtime.DispatchToolResultLifecycleForTest(context.Background(), toolEvent))

	require.Len(t, *toolCallDiagnostics, 1)
	assert.Equal(t, 1, (*toolCallDiagnostics)[0]["hook_count"])
	assert.Equal(t, testToolName, (*toolCallDiagnostics)[0]["name"])
	assert.Equal(t, []string{"path"}, (*toolCallDiagnostics)[0]["argument_keys"])
	require.Len(t, *toolResultDiagnostics, 1)
	assert.Equal(t, 1, (*toolResultDiagnostics)[0]["hook_count"])
	assert.Equal(t, testToolName, (*toolResultDiagnostics)[0]["name"])
	assert.Equal(t, false, (*toolResultDiagnostics)[0]["has_error"])
	assert.Equal(t, len("redacted"), (*toolResultDiagnostics)[0]["result_bytes"])
}

func collectRuntimeDiagnosticPayloads(t *testing.T, bus *event.Bus, channel string) *[]map[string]any {
	t.Helper()

	payloads := []map[string]any{}
	subscription := bus.Channel(channel).Subscribe(ro.NewObserver(
		func(envelope event.Envelope) {
			payload, ok := envelope.Data.(map[string]any)
			if ok {
				payloads = append(payloads, payload)
			}
		},
		func(error) {},
		func() {},
	))
	t.Cleanup(subscription.Unsubscribe)

	return &payloads
}
