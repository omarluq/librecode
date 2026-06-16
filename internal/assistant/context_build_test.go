package assistant_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRuntime_ContextBuildIncludesAgentInstructions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("LIBRECODE_HOME", filepath.Join(t.TempDir(), ".librecode"))

	cwd := t.TempDir()
	writeRuntimeTestFile(t, filepath.Join(cwd, "AGENTS.md"), "Always follow project instructions.")

	client := &capturingCompleter{request: nil}
	runtime, _, _ := newTestRuntimeWithManager(t, client)
	_, err := runtime.Prompt(context.Background(), newRuntimePromptRequest(cwd, "hello", ""))

	require.NoError(t, err)
	require.NotNil(t, client.request)
	assert.Contains(t, client.request.SystemPrompt, "Always follow project instructions.")
}

func TestRuntime_ContextBuildAcceptsBoundedExtensionContributions(t *testing.T) {
	t.Parallel()

	client := &capturingCompleter{request: nil}
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
	assert.Positive(t, client.request.Usage.Breakdown["extensions"])
}

func TestRuntime_ContextBuildRejectsOversizedExtensionContributions(t *testing.T) {
	t.Parallel()

	runtime, _, manager := newTestRuntimeWithManager(t, testCompleter{})
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

	runtime, _, manager := newTestRuntimeWithManager(t, testCompleter{})
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
