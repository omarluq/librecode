package extension_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/extension"
)

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
lc.on("before_provider_request", function(event)
  event.payload.payload.marker = "changed"
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
	assert.Equal(t, map[string]string{"X-Debug": "yes"}, result.ProviderRequest.Headers)
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
