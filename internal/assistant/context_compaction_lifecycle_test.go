package assistant_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/event"
	"github.com/omarluq/librecode/internal/extension"
)

func TestRuntime_CompactionLifecycleCanProvideSummary(t *testing.T) {
	t.Parallel()

	client := &compactionCompletionClient{summary: autoCompactionTestUnused, requests: nil}
	_, repository, manager := newTestRuntimeWithManager(t, client)
	runtime := newCompactionRuntimeWithManager(t, repository, manager, client, 1)
	loadRuntimeExtension(t, manager, `
local lc = require("librecode")
lc.on("session_before_compact", function(event)
  return {
    compaction = {
      summary = "extension summary for " .. event.payload.first_kept_entry_id,
      details = { origin = "extension" },
    }
  }
end)
`)
	ctx := context.Background()
	session, err := repository.CreateSession(ctx, testRuntimeCWD, "compact", "")
	require.NoError(t, err)
	old := appendRuntimeTestMessage(t, repository, session.ID, nil, database.RoleUser, strings.Repeat("old ", 1000))
	appendRuntimeTestMessage(t, repository, session.ID, &old.ID, database.RoleAssistant, "tail")

	entry, err := runtime.CompactSession(ctx, session.ID, testRuntimeCWD)

	require.NoError(t, err)
	assert.Empty(t, client.requests)
	assert.Contains(t, entry.Summary, "extension summary")
	data := map[string]any{}
	require.NoError(t, json.Unmarshal([]byte(entry.DataJSON), &data))
	fromHook, ok := data["fromHook"].(bool)
	require.True(t, ok)
	assert.True(t, fromHook)
	details, ok := data["details"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "extension", details["origin"])
}

func TestRuntime_CompactionLifecycleCanCancel(t *testing.T) {
	t.Parallel()

	client := &compactionCompletionClient{summary: autoCompactionTestUnused, requests: nil}
	_, repository, manager := newTestRuntimeWithManager(t, client)
	runtime := newCompactionRuntimeWithManager(t, repository, manager, client, 1)
	loadRuntimeExtension(t, manager, `
local lc = require("librecode")
lc.on("session_before_compact", function()
  return { compaction = { cancel = true } }
end)
`)
	ctx := context.Background()
	session, err := repository.CreateSession(ctx, testRuntimeCWD, "compact", "")
	require.NoError(t, err)
	old := appendRuntimeTestMessage(t, repository, session.ID, nil, database.RoleUser, strings.Repeat("old ", 1000))
	appendRuntimeTestMessage(t, repository, session.ID, &old.ID, database.RoleAssistant, "tail")

	entry, err := runtime.CompactSession(ctx, session.ID, testRuntimeCWD)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "canceled by an extension")
	assert.Nil(t, entry)
	assert.Empty(t, client.requests)
}

func TestRuntime_CompactionLifecyclePublishesSavedEvent(t *testing.T) {
	t.Parallel()

	client := &compactionCompletionClient{summary: compactedWorkSummary, requests: nil}
	_, repository, manager := newTestRuntimeWithManager(t, client)
	runtime := newCompactionRuntimeWithManager(t, repository, manager, client, 1)
	loadRuntimeExtension(t, manager, `
local lc = require("librecode")
local saved = ""
lc.on("session_compact", function(event)
  saved = event.payload.entry_id .. ":" .. event.payload.source
end)
lc.register_command("saved_compaction", "saved_compaction", function()
  return saved
end)
`)
	ctx := context.Background()
	session, err := repository.CreateSession(ctx, testRuntimeCWD, "compact", "")
	require.NoError(t, err)
	old := appendRuntimeTestMessage(t, repository, session.ID, nil, database.RoleUser, strings.Repeat("old ", 1000))
	appendRuntimeTestMessage(t, repository, session.ID, &old.ID, database.RoleAssistant, "tail")

	entry, err := runtime.CompactSession(ctx, session.ID, testRuntimeCWD)
	require.NoError(t, err)
	saved, err := manager.ExecuteCommand(ctx, "saved_compaction", "")

	require.NoError(t, err)
	assert.Equal(t, entry.ID+":core", saved)
}

func newCompactionRuntimeWithManager(
	t *testing.T,
	repository *database.SessionRepository,
	manager *extension.Manager,
	client assistant.CompletionClient,
	keepRecentTokens int,
) *assistant.Runtime {
	t.Helper()

	runtimeConfig := testConfig()
	runtimeConfig.Context.KeepRecentTokens = keepRecentTokens
	cache := assistant.NewResponseCache(true, 32, time.Minute)
	t.Cleanup(cache.Shutdown)

	return assistant.NewRuntime(
		runtimeConfig,
		repository,
		manager,
		cache,
		event.NewBus(slog.New(slog.NewTextHandler(io.Discard, nil))),
		testRegistry(t),
		client,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
}
