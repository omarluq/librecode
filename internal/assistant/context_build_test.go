package assistant_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samber/ro"

	"github.com/omarluq/librecode/internal/event"
	"github.com/omarluq/librecode/internal/extension"
)

func TestRuntime_ContextBuildAcceptsBoundedExtensionContributions(t *testing.T) {
	t.Parallel()

	client := &capturingCompletionClient{request: nil}
	runtime, _, manager := newTestRuntimeWithManager(t, client)
	loadRuntimeExtension(t, manager, `
local lc = require("librecode")
lc.on("context_build", function(event)
  event.payload.contributions = {
    {
      name = "project-note",
      source = "test-extension",
      role = "system",
      content = "Always mention extension context when relevant.",
      metadata = { reason = "test" },
    },
  }
  return { payload = event.payload }
end)
`)

	_, err := runtime.Prompt(context.Background(), newRuntimePromptRequest(testRuntimeCWD, "context", ""))

	require.NoError(t, err)
	require.NotNil(t, client.request)
	assert.Contains(t, client.request.SystemPrompt, "<extension_context>")
	assert.Contains(t, client.request.SystemPrompt, "project-note")
	assert.Contains(t, client.request.SystemPrompt, "Always mention extension context")
	require.NotNil(t, client.request.Usage.Breakdown)
	assert.Greater(t, client.request.Usage.Breakdown["extensions"], 0)
}

func TestRuntime_ContextBuildRejectsOversizedExtensionContributions(t *testing.T) {
	t.Parallel()

	runtime, _, manager := newTestRuntimeWithManager(t, testCompletionClient{})
	loadRuntimeExtension(t, manager, `
local lc = require("librecode")
lc.on("context_build", function(event)
  event.payload.contributions = {
    { name = "huge", content = string.rep("token ", 9000) },
  }
  return { payload = event.payload }
end)
`)

	_, err := runtime.Prompt(context.Background(), newRuntimePromptRequest(testRuntimeCWD, "context", ""))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "contribution limit")
}

func TestRuntime_ContextBuildPayloadContainsBreakdown(t *testing.T) {
	t.Parallel()

	runtime, _, manager := newTestRuntimeWithManager(t, testCompletionClient{})
	loadRuntimeExtension(t, manager, `
local lc = require("librecode")
local seen = ""
lc.on("context_build", function(event)
  local breakdown = event.payload.breakdown or {}
  seen = table.concat({
    tostring(breakdown.system ~= nil),
    tostring(breakdown.skills ~= nil),
    tostring(breakdown.history ~= nil),
    tostring(breakdown.extensions ~= nil),
    tostring(event.payload.max_contribution_tokens ~= nil),
  }, ":")
end)
lc.register_command("context_seen", "context_seen", function()
  return seen
end)
`)

	request := newRuntimePromptRequest(testRuntimeCWD, strings.Repeat("hello ", 3), "")
	_, err := runtime.Prompt(context.Background(), request)
	require.NoError(t, err)

	seen, err := manager.ExecuteCommand(context.Background(), "context_seen", "")
	require.NoError(t, err)
	assert.Equal(t, "true:true:true:true:true", seen)
}

func TestRuntime_ContextBuildPublishesContributionBreakdown(t *testing.T) {
	t.Parallel()

	client := &capturingCompletionClient{request: nil}
	runtime, _, manager := newTestRuntimeWithManager(t, client)
	loadRuntimeExtension(t, manager, `
local lc = require("librecode")
lc.on("context_build", function(event)
  event.payload.contributions = {
    { name = "note", content = "extra context" },
  }
  return { payload = event.payload }
end)
`)

	var payload map[string]any
	subscription := runtime.EventBus().Channel(string(extension.LifecycleContextBuild)).Subscribe(ro.NewObserver(
		func(envelope event.Envelope) {
			data, ok := envelope.Data.(map[string]any)
			if ok {
				payload = data
			}
		},
		func(err error) {
			t.Errorf("unexpected context_build stream error: %v", err)
		},
		func() {
			// Test subscription should not complete before cleanup.
		},
	))
	defer subscription.Unsubscribe()

	_, err := runtime.Prompt(context.Background(), newRuntimePromptRequest(testRuntimeCWD, "context", ""))

	require.NoError(t, err)
	require.NotNil(t, payload)
	breakdown, ok := payload["breakdown"].(map[string]any)
	require.True(t, ok)
	extensionTokens, ok := breakdown["extensions"].(int)
	require.True(t, ok)
	assert.Greater(t, extensionTokens, 0)
}
