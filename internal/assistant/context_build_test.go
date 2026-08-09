package assistant_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/testutil"
)

func TestRuntime_ContextBuildUsesPromptBranchEndpoint(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := &contextRequestChannelCompleter{requests: make(chan *assistant.CompletionRequest, 2)}
	runtime, repository, _ := newTestRuntimeWithManager(t, client)
	session, err := repository.CreateSession(ctx, testRuntimeCWD, "branch context", "")
	require.NoError(t, err)
	root := appendRuntimeTestMessage(t, repository, session.ID, nil, database.RoleUser, "root")

	firstPersisted := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstRequest := newRuntimePromptRequest(testRuntimeCWD, "first branch", "")
	firstRequest.SessionID = session.ID
	firstRequest.ParentEntryID = &root.ID
	firstRequest.OnUserEntry = func(assistant.PromptUserEntryEvent) {
		close(firstPersisted)
		<-releaseFirst
	}

	firstDone := make(chan error, 1)

	go func() {
		_, promptErr := runtime.Prompt(ctx, firstRequest)
		firstDone <- promptErr
	}()

	select {
	case <-firstPersisted:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "timed out waiting for first prompt persistence")
	}

	secondRequest := newRuntimePromptRequest(testRuntimeCWD, "second branch", "")
	secondRequest.SessionID = session.ID
	secondRequest.ParentEntryID = &root.ID
	secondDone := make(chan error, 1)

	go func() {
		_, promptErr := runtime.Prompt(ctx, secondRequest)
		secondDone <- promptErr
	}()

	close(releaseFirst)

	firstProviderRequest := receiveContextRequest(t, client.requests)
	require.NoError(t, <-firstDone)
	assert.Equal(t, []string{"root", "first branch"}, contextRequestContents(firstProviderRequest))

	secondProviderRequest := receiveContextRequest(t, client.requests)
	require.NoError(t, <-secondDone)
	assert.Equal(t, []string{"root", "second branch"}, contextRequestContents(secondProviderRequest))
}

func TestRuntime_ContextBuildIncludesAgentInstructions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	testutil.SetWindowsHome(t, home)
	t.Setenv("LIBRECODE_HOME", filepath.Join(home, ".librecode"))

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

type contextRequestChannelCompleter struct {
	requests chan *assistant.CompletionRequest
}

func (client *contextRequestChannelCompleter) Complete(
	_ context.Context,
	request *assistant.CompletionRequest,
) (*assistant.CompletionResult, error) {
	client.requests <- request

	return testCompletionResult("ok"), nil
}

func receiveContextRequest(
	t *testing.T,
	requests <-chan *assistant.CompletionRequest,
) *assistant.CompletionRequest {
	t.Helper()

	select {
	case request := <-requests:
		return request
	case <-time.After(5 * time.Second):
		require.FailNow(t, "timed out waiting for completion request")

		return nil
	}
}

func contextRequestContents(request *assistant.CompletionRequest) []string {
	contents := make([]string, 0, len(request.Messages))
	for index := range request.Messages {
		contents = append(contents, request.Messages[index].Content)
	}

	return contents
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
