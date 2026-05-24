package assistant_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
)

func TestRuntime_ToolCallLifecycleAppliesArgumentMutation(t *testing.T) {
	t.Parallel()

	runtime, _, manager := newTestRuntimeWithManager(t, testCompletionClient{})
	loadRuntimeExtension(t, manager, `
local lc = require("librecode")
lc.on("tool_call", function(event)
  return {
    tool_call = {
      arguments = {
        path = "changed.txt",
        limit = 3,
      },
    },
  }
end)
`)
	call := assistant.ToolCallEvent{
		Arguments:     map[string]any{testToolPathKey: "README.md"},
		ID:            "call-1",
		Name:          testToolName,
		ArgumentsJSON: testToolArgsJSON,
	}

	err := runtime.DispatchToolCallLifecycleForTest(context.Background(), &call)

	require.NoError(t, err)
	assert.Equal(t, "changed.txt", call.Arguments[testToolPathKey])
	assert.Equal(t, float64(3), call.Arguments["limit"])
	assert.JSONEq(t, `{"limit":3,"path":"changed.txt"}`, call.ArgumentsJSON)
}

func TestRuntime_ToolResultLifecycleAppliesResultMutation(t *testing.T) {
	t.Parallel()

	runtime, _, manager := newTestRuntimeWithManager(t, testCompletionClient{})
	loadRuntimeExtension(t, manager, `
local lc = require("librecode")
lc.on("tool_result", function(event)
  return {
    tool_result = {
      result = "redacted",
      details_json = "{\"redacted\":true}",
      error = "",
    },
  }
end)
`)
	event := &assistant.ToolEvent{
		Name:          testToolName,
		ArgumentsJSON: testToolArgsJSON,
		DetailsJSON:   "{}",
		Result:        "secret",
		Error:         "boom",
	}

	err := runtime.DispatchToolResultLifecycleForTest(context.Background(), event)

	require.NoError(t, err)
	assert.Equal(t, "redacted", event.Result)
	assert.JSONEq(t, `{"redacted":true}`, event.DetailsJSON)
	assert.Empty(t, event.Error)
}
