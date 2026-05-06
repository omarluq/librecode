package session_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sqlite "modernc.org/sqlite"

	"github.com/omarluq/librecode/internal/session"
)

func TestStore_AppendsMessagesInSessionTree(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	createdSession, err := store.CreateSession(ctx, "/work", "port", "")
	require.NoError(t, err)

	firstEntry, err := store.AppendMessage(ctx, createdSession.ID, nil, session.Message{
		Role:      session.RoleUser,
		Content:   "hello",
		Provider:  "",
		Model:     "",
		Timestamp: time.Now().UTC(),
	})
	require.NoError(t, err)

	secondEntry, err := store.AppendMessage(ctx, createdSession.ID, &firstEntry.ID, session.Message{
		Role:      session.RoleAssistant,
		Content:   "hi",
		Provider:  "local",
		Model:     "librecode-go",
		Timestamp: time.Now().UTC(),
	})
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

func newTestStore(t *testing.T) *session.Store {
	t.Helper()

	database, err := sql.Open(sqliteDriver(), ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, database.Close())
	})
	database.SetMaxOpenConns(1)

	require.NoError(t, session.Migrate(context.Background(), database))

	return session.NewStore(database)
}

func sqliteDriver() string {
	var driverError *sqlite.Error
	_ = driverError

	return "sqlite"
}
