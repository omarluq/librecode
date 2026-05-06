package database_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/omarluq/librecode/internal/database"
)

func TestStore_AppendsMessagesInSessionTree(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	createdSession, err := store.CreateSession(ctx, "/work", "port", "")
	require.NoError(t, err)

	firstMessage := database.MessageEntity{
		Timestamp: time.Now().UTC(),
		Role:      database.RoleUser,
		Content:   "hello",
		Provider:  "",
		Model:     "",
	}
	firstEntry, err := store.AppendMessage(ctx, createdSession.ID, nil, &firstMessage)
	require.NoError(t, err)

	secondMessage := database.MessageEntity{
		Timestamp: time.Now().UTC(),
		Role:      database.RoleAssistant,
		Content:   "hi",
		Provider:  "local",
		Model:     "librecode-go",
	}
	secondEntry, err := store.AppendMessage(ctx, createdSession.ID, &firstEntry.ID, &secondMessage)
	require.NoError(t, err)

	latestSession, found, err := store.LatestSession(ctx, "/work")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, createdSession.ID, latestSession.ID)

	leafEntry, found, err := store.LeafEntry(ctx, createdSession.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, secondEntry.ID, leafEntry.ID)

	entries, err := store.Entries(ctx, createdSession.ID)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Nil(t, entries[0].ParentID)
	require.NotNil(t, entries[1].ParentID)
	assert.Equal(t, firstEntry.ID, *entries[1].ParentID)
}

func newTestStore(t *testing.T) *database.SessionStore {
	t.Helper()

	connection, err := sql.Open(sqliteDriver(), ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, connection.Close())
	})
	connection.SetMaxOpenConns(1)

	require.NoError(t, database.Migrate(context.Background(), connection))

	return database.NewSessionStore(connection)
}

func sqliteDriver() string {
	return "sqlite"
}
