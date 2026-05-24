package extension_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/extension"
)

const testLifecyclePathKey = "path"

func TestManager_DispatchLifecycleRunsHandlersInPriorityOrder(t *testing.T) {
	t.Parallel()

	manager := loadTestExtension(t, `
local lc = require("librecode")
lc.on("turn_start", { priority = 1 }, function(event)
  event.payload.order = event.payload.order .. "b"
  return { payload = event.payload }
end)
lc.on("turn_start", { priority = 10 }, function(event)
  event.payload.order = event.payload.order .. "a"
  return { payload = event.payload }
end)
`)
	result, err := manager.DispatchLifecycle(context.Background(), extension.LifecycleEvent{
		Name:    extension.LifecycleTurnStart,
		Payload: map[string]any{"order": ""},
	})

	require.NoError(t, err)
	assert.Equal(t, "turn_start", result.Name)
	assert.Equal(t, "ab", result.Payload["order"])
	assert.Equal(t, 2, result.HandlerCount)
	assert.Empty(t, result.Errors)
	assert.False(t, result.Consumed)
	assert.False(t, result.Stopped)
	assert.Positive(t, result.Duration)
}

func TestManager_DispatchLifecycleCanStopHandlers(t *testing.T) {
	t.Parallel()

	manager := loadTestExtension(t, `
local lc = require("librecode")
lc.on("context_build", { priority = 10 }, function(event)
  event.payload.count = event.payload.count + 1
  return { payload = event.payload, stop = true }
end)
lc.on("context_build", { priority = 1 }, function(event)
  event.payload.count = event.payload.count + 100
  return { payload = event.payload }
end)
`)
	result, err := manager.DispatchLifecycle(context.Background(), extension.LifecycleEvent{
		Name:    extension.LifecycleContextBuild,
		Payload: map[string]any{"count": 0},
	})

	require.NoError(t, err)
	assert.Equal(t, 1.0, result.Payload["count"])
	assert.Equal(t, 1, result.HandlerCount)
	assert.True(t, result.Consumed)
	assert.True(t, result.Stopped)
}

func TestManager_DispatchLifecycleCollectsHandlerErrors(t *testing.T) {
	t.Parallel()

	manager := loadTestExtension(t, `
local lc = require("librecode")
lc.on("provider_error", function()
  error("boom")
end)
`)
	result, err := manager.DispatchLifecycle(context.Background(), extension.LifecycleEvent{
		Name:    extension.LifecycleProviderError,
		Payload: map[string]any{"provider": "test"},
	})

	require.Error(t, err)
	assert.Equal(t, 1, result.HandlerCount)
	require.Len(t, result.Errors, 1)
	assert.Contains(t, result.Errors[0], "boom")
}

func TestManager_DispatchLifecycleCollectsProviderRequestMutation(t *testing.T) {
	t.Parallel()

	manager := loadTestExtension(t, `
local lc = require("librecode")
lc.on("before_provider_request", { priority = 10 }, function(event)
  event.payload.payload.marker = "changed"
  return {
    payload = event.payload,
    provider_request = {
      headers = {
        ["X-Debug"] = "yes",
        ["X-Shared"] = "first",
      },
    },
  }
end)
lc.on("before_provider_request", { priority = 1 }, function(event)
  return {
    provider_request = {
      headers = {
        ["X-Trace"] = "trace-id",
        ["X-Shared"] = "second",
      },
    },
  }
end)
`)
	result, err := manager.DispatchLifecycle(context.Background(), extension.LifecycleEvent{
		Name: extension.LifecycleBeforeProviderRequest,
		Payload: map[string]any{
			"payload": map[string]any{},
		},
	})

	require.NoError(t, err)
	payload, ok := result.Payload["payload"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "changed", payload["marker"])
	assert.Equal(t, map[string]string{
		"X-Debug":  "yes",
		"X-Shared": "second",
		"X-Trace":  "trace-id",
	}, result.ProviderRequest.Headers)
}

func TestManager_DispatchLifecycleCollectsToolMutations(t *testing.T) {
	t.Parallel()

	manager := loadTestExtension(t, `
local lc = require("librecode")
lc.on("tool_call", { priority = 10 }, function(event)
  return {
    tool_call = {
      arguments = {
        path = "changed.txt",
      },
    },
  }
end)
lc.on("tool_call", { priority = 1 }, function(event)
  return {
    tool_call = {
      arguments = {
        limit = 5,
      },
    },
  }
end)
lc.on("tool_result", function(event)
  return {
    tool_result = {
      result = "filtered",
      details_json = "{\"filtered\":true}",
      error = "",
    },
  }
end)
`)
	callResult, err := manager.DispatchLifecycle(context.Background(), extension.LifecycleEvent{
		Name: extension.LifecycleToolCall,
		Payload: map[string]any{
			"name":      "read",
			"arguments": map[string]any{testLifecyclePathKey: "README.md"},
		},
	})
	require.NoError(t, err)
	assert.Equal(
		t,
		map[string]any{"limit": float64(5), testLifecyclePathKey: "changed.txt"},
		callResult.ToolCall.Arguments,
	)

	resultResult, err := manager.DispatchLifecycle(context.Background(), extension.LifecycleEvent{
		Name: extension.LifecycleToolResult,
		Payload: map[string]any{
			"name":   "read",
			"result": "contents",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, resultResult.ToolResult.Result)
	require.NotNil(t, resultResult.ToolResult.DetailsJSON)
	require.NotNil(t, resultResult.ToolResult.Error)
	assert.Equal(t, "filtered", *resultResult.ToolResult.Result)
	assert.JSONEq(t, `{"filtered":true}`, *resultResult.ToolResult.DetailsJSON)
	assert.Empty(t, *resultResult.ToolResult.Error)
}

func TestManager_DispatchLifecycleRequiresName(t *testing.T) {
	t.Parallel()

	manager := extension.NewManager(nil)
	t.Cleanup(manager.Shutdown)

	_, err := manager.DispatchLifecycle(context.Background(), extension.LifecycleEvent{
		Name:    "",
		Payload: map[string]any{},
	})

	require.Error(t, err)
}
