package database_test

import (
	"context"
	"testing"

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

	compactionEntry := appendTestCompactionSimple(
		ctx, t, repository,
		session.ID, &rootEntry.ID,
		"summary", rootEntry.ID, 1234,
	)
	assert.True(t, compactionEntry.ModelFacing)
	assert.True(t, compactionEntry.Display)
	assert.Equal(t, rootEntry.ID, compactionEntry.CompactionFirstKeptEntryID)
	assert.Equal(t, 1234, compactionEntry.CompactionTokensBefore)
	assert.Positive(t, compactionEntry.TokenEstimate)
}
