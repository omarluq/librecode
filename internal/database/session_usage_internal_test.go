package database

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite" // register SQLite driver for sql.Open in this test
)

func newTestSessionRepositoryForUsage(t *testing.T) *SessionRepository {
	t.Helper()

	connection, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, connection.Close())
	})
	connection.SetMaxOpenConns(1)

	require.NoError(t, Migrate(context.Background(), connection))

	return NewSessionRepository(connection)
}

func TestBuildContextReadsLegacyCamelCaseUsage(t *testing.T) {
	t.Parallel()

	repository := newTestSessionRepositoryForUsage(t)
	ctx := context.Background()
	session, err := repository.CreateSession(ctx, "/work", "usage", "")
	require.NoError(t, err)

	entry := newEntryEntity(session.ID, nil, EntryTypeMessage, &MessageEntity{
		Timestamp: time.Now().UTC(),
		Role:      RoleAssistant,
		Content:   "answer",
		Provider:  "openai",
		Model:     "gpt",
	})
	entry.ModelFacing = true
	require.NoError(t, repository.appendEntry(ctx, &entry))
	_, err = repository.sql.Exec(
		ctx,
		`UPDATE session_entries SET data_json = ? WHERE id = ?`,
		`{"usage":{"contextWindow":1000,"contextTokens":300,"inputTokens":250,"outputTokens":50}}`,
		entry.ID,
	)
	require.NoError(t, err)

	contextEntity, err := repository.BuildContext(ctx, session.ID, entry.ID)
	require.NoError(t, err)
	require.NotNil(t, contextEntity.UsageAnchor)
	assert.Equal(t, 1000, contextEntity.UsageAnchor.Usage.ContextWindow)
	assert.Equal(t, 300, contextEntity.UsageAnchor.Usage.ContextTokens)
	assert.Equal(t, 250, contextEntity.UsageAnchor.Usage.InputTokens)
	assert.Equal(t, 50, contextEntity.UsageAnchor.Usage.OutputTokens)
}

func TestBuildContextReturnsAssistantUsageDecodeError(t *testing.T) {
	t.Parallel()

	repository := newTestSessionRepositoryForUsage(t)
	ctx := context.Background()
	session, err := repository.CreateSession(ctx, "/work", "usage", "")
	require.NoError(t, err)

	entry := newEntryEntity(session.ID, nil, EntryTypeMessage, &MessageEntity{
		Timestamp: time.Now().UTC(),
		Role:      RoleAssistant,
		Content:   "answer",
		Provider:  "openai",
		Model:     "gpt",
	})
	entry.ModelFacing = true
	require.NoError(t, repository.appendEntry(ctx, &entry))
	_, err = repository.sql.Exec(
		ctx,
		`UPDATE session_entries SET data_json = ? WHERE id = ?`,
		`{"usage":`,
		entry.ID,
	)
	require.NoError(t, err)

	_, err = repository.BuildContext(ctx, session.ID, entry.ID)
	require.Error(t, err)

	var oopsError oops.OopsError
	require.ErrorAs(t, err, &oopsError)
	assert.Equal(t, "decode_entry_usage", oopsError.Code())
	assert.Contains(t, err.Error(), "decode assistant entry usage")
}
