package database_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

func TestSessionRepository_EnrichesEntryMetadata(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := newTestSessionRepository(t)
	session, err := repository.CreateSession(ctx, "/work", "metadata", "")
	require.NoError(t, err)

	userEntry := appendTestMessage(ctx, t, repository, session.ID, nil, database.RoleUser, "hello world")
	assert.True(t, userEntry.ModelFacing)
	assert.True(t, userEntry.Display)
	assert.Positive(t, userEntry.TokenEstimate)
	assert.Empty(t, userEntry.ToolName)

	toolEntry := appendTestMessage(
		ctx,
		t,
		repository,
		session.ID,
		&userEntry.ID,
		database.RoleToolResult,
		"tool: read\narguments: {\"path\":\"main.go\"}\noutput:\npackage main\n",
	)
	assert.False(t, toolEntry.ModelFacing)
	assert.True(t, toolEntry.Display)
	assert.Equal(t, "read", toolEntry.ToolName)
	assert.Equal(t, "success", toolEntry.ToolStatus)
	assert.JSONEq(t, `{"path":"main.go"}`, toolEntry.ToolArgsJSON)
	assert.Positive(t, toolEntry.TokenEstimate)

	fetched, found, err := repository.Entry(ctx, session.ID, toolEntry.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, toolEntry.ToolName, fetched.ToolName)
	assert.Equal(t, toolEntry.ToolStatus, fetched.ToolStatus)
	assert.Equal(t, toolEntry.ToolArgsJSON, fetched.ToolArgsJSON)
	assert.Equal(t, toolEntry.TokenEstimate, fetched.TokenEstimate)
	assert.Equal(t, toolEntry.ModelFacing, fetched.ModelFacing)
	assert.Equal(t, toolEntry.Display, fetched.Display)
}

func TestSessionRepository_EnrichesCompactionMetadata(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := newTestSessionRepository(t)
	session, err := repository.CreateSession(ctx, "/work", "compaction", "")
	require.NoError(t, err)
	rootEntry := appendTestMessage(ctx, t, repository, session.ID, nil, database.RoleUser, "hello")

	compactionEntry, err := repository.AppendCompaction(
		ctx,
		session.ID,
		&rootEntry.ID,
		"summary",
		rootEntry.ID,
		1234,
		nil,
		false,
	)
	require.NoError(t, err)
	assert.True(t, compactionEntry.ModelFacing)
	assert.True(t, compactionEntry.Display)
	assert.Equal(t, rootEntry.ID, compactionEntry.CompactionFirstKeptEntryID)
	assert.Equal(t, 1234, compactionEntry.CompactionTokensBefore)
	assert.Positive(t, compactionEntry.TokenEstimate)
}

func TestSessionRepository_BackfillsEntryMetadataMigration(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	connection := newMigratedThroughVersion(t, 3)
	repository := database.NewSessionRepository(connection)
	session, err := repository.CreateSession(ctx, "/work", "legacy", "")
	require.NoError(t, err)

	createdAt := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = connection.ExecContext(
		ctx,
		`INSERT INTO session_entries (
			id, session_id, parent_id, entry_type, role, content,
			provider, model, custom_type, data_json, summary, created_at
		) VALUES (?, ?, NULL, ?, ?, ?, '', '', '', '{}', '', ?)`,
		"legacy-tool",
		session.ID,
		string(database.EntryTypeMessage),
		string(database.RoleToolResult),
		"tool: bash\narguments: {\"command\":\"echo hi\"}\nerror:\nboom\n",
		createdAt,
	)
	require.NoError(t, err)

	require.NoError(t, database.Migrate(ctx, connection))

	entry, found, err := repository.Entry(ctx, session.ID, "legacy-tool")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "bash", entry.ToolName)
	assert.Equal(t, "error", entry.ToolStatus)
	assert.JSONEq(t, `{"command":"echo hi"}`, entry.ToolArgsJSON)
	assert.False(t, entry.ModelFacing)
	assert.True(t, entry.Display)
	assert.Positive(t, entry.TokenEstimate)
}
