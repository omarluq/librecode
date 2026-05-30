package assistant_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRuntime_ContextBuildTracksTopContributors(t *testing.T) {
	t.Parallel()

	client := &capturingCompletionClient{request: nil}
	runtime, _, _ := newTestRuntimeWithManager(t, client)
	prompt := strings.Repeat("large prompt ", 400)

	_, err := runtime.Prompt(context.Background(), newRuntimePromptRequest(testRuntimeCWD, prompt, ""))

	require.NoError(t, err)
	require.NotNil(t, client.request)
	require.NotEmpty(t, client.request.Usage.TopContributors)
	var foundUser bool
	for _, contributor := range client.request.Usage.TopContributors {
		if contributor.Label == "message 1" && contributor.Role == "user" {
			foundUser = true
			assert.Greater(t, contributor.Tokens, 0)
			assert.NotEmpty(t, contributor.Preview)
		}
	}
	assert.True(t, foundUser, "expected user prompt in top contributors")
}

func TestRuntime_ContextBuildTopContributorsIncludeExtensionContext(t *testing.T) {
	t.Parallel()

	client := &capturingCompletionClient{request: nil}
	runtime, _, manager := newTestRuntimeWithManager(t, client)
	loadRuntimeExtension(t, manager, `
local lc = require("librecode")
lc.on("context_build", function(event)
  event.payload.contributions = {
    { name = "big-note", content = string.rep("extension context ", 200) },
  }
  return { payload = event.payload }
end)
`)

	_, err := runtime.Prompt(context.Background(), newRuntimePromptRequest(testRuntimeCWD, "hello", ""))

	require.NoError(t, err)
	require.NotNil(t, client.request)
	assert.Contains(t, client.request.SystemPrompt, "big-note")
	assert.Contains(t, client.request.Usage.Breakdown, "extensions")
	require.NotEmpty(t, client.request.Usage.TopContributors)

	var foundExtension bool
	for _, contributor := range client.request.Usage.TopContributors {
		if contributor.Label == "big-note" {
			foundExtension = true
			assert.Greater(t, contributor.Tokens, 0)
			assert.Contains(t, contributor.Preview, "extension context")
		}
	}
	assert.True(t, foundExtension, "expected extension contribution in top contributors")
}
