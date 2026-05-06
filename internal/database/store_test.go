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

func TestStore_SupportsPiStyleTreeMetadata(t *testing.T) {
	t.Parallel()

	fixture := newPiMetadataFixture(t)

	foundLabel, found, err := fixture.store.Label(fixture.ctx, fixture.sessionID, fixture.userEntry.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, fixture.label, foundLabel)

	branch, err := fixture.store.Branch(fixture.ctx, fixture.sessionID, fixture.branchEntry.ID)
	require.NoError(t, err)
	require.Len(t, branch, 2)
	assert.Equal(t, fixture.userEntry.ID, branch[0].ID)
	assert.Equal(t, fixture.branchEntry.ID, branch[1].ID)

	tree, err := fixture.store.Tree(fixture.ctx, fixture.sessionID)
	require.NoError(t, err)
	require.Len(t, tree, 1)
	assert.Equal(t, fixture.userEntry.ID, tree[0].Entry.ID)
	require.Len(t, tree[0].Children, 2)

	contextEntity, err := fixture.store.BuildContext(fixture.ctx, fixture.sessionID, fixture.compactionEntry.ID)
	require.NoError(t, err)
	assert.Equal(t, "anthropic", contextEntity.Provider)
	assert.Equal(t, "sonnet", contextEntity.Model)
	assert.Equal(t, "high", contextEntity.ThinkingLevel)
	require.Len(t, contextEntity.Messages, 1)
	assert.Equal(t, database.RoleCompactionSummary, contextEntity.Messages[0].Role)
	assert.Equal(t, "summary of earlier work", contextEntity.Messages[0].Content)

	_, err = fixture.store.AppendSessionInfo(
		fixture.ctx,
		fixture.sessionID,
		&fixture.compactionEntry.ID,
		"named session",
	)
	require.NoError(t, err)
	updatedSession, found, err := fixture.store.GetSession(fixture.ctx, fixture.sessionID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "named session", updatedSession.Name)
}

type piMetadataFixture struct {
	ctx             context.Context
	store           *database.SessionStore
	userEntry       *database.EntryEntity
	branchEntry     *database.EntryEntity
	compactionEntry *database.EntryEntity
	sessionID       string
	label           string
}

func newPiMetadataFixture(t *testing.T) piMetadataFixture {
	t.Helper()

	store := newTestStore(t)
	ctx := context.Background()
	createdSession, err := store.CreateSession(ctx, "/work", "", "")
	require.NoError(t, err)

	userEntry := appendTestMessage(ctx, t, store, createdSession.ID, nil, database.RoleUser, "pick a path")
	modelEntry, err := store.AppendModelChange(ctx, createdSession.ID, &userEntry.ID, "anthropic", "sonnet")
	require.NoError(t, err)
	thinkingEntry, err := store.AppendThinkingLevelChange(ctx, createdSession.ID, &modelEntry.ID, "high")
	require.NoError(t, err)
	assistantEntry := appendTestMessage(
		ctx,
		t,
		store,
		createdSession.ID,
		&thinkingEntry.ID,
		database.RoleAssistant,
		"path A",
	)

	label := "checkpoint"
	labelEntry, err := store.AppendLabelChange(ctx, createdSession.ID, &assistantEntry.ID, userEntry.ID, &label)
	require.NoError(t, err)
	customEntry, err := store.AppendCustomMessage(
		ctx,
		createdSession.ID,
		&labelEntry.ID,
		"test-extension",
		"extra",
		true,
		nil,
	)
	require.NoError(t, err)
	compactionEntry, err := store.AppendCompaction(
		ctx,
		createdSession.ID,
		&customEntry.ID,
		"summary of earlier work",
		assistantEntry.ID,
		1200,
		nil,
		false,
	)
	require.NoError(t, err)
	branchEntry, err := store.AppendBranchSummary(
		ctx,
		createdSession.ID,
		&userEntry.ID,
		compactionEntry.ID,
		"branch",
		nil,
		false,
	)
	require.NoError(t, err)

	return piMetadataFixture{
		ctx:             ctx,
		store:           store,
		userEntry:       userEntry,
		branchEntry:     branchEntry,
		compactionEntry: compactionEntry,
		sessionID:       createdSession.ID,
		label:           label,
	}
}

func appendTestMessage(
	ctx context.Context,
	t *testing.T,
	store *database.SessionStore,
	sessionID string,
	parentID *string,
	role database.Role,
	content string,
) *database.EntryEntity {
	t.Helper()

	message := database.MessageEntity{
		Timestamp: time.Now().UTC(),
		Role:      role,
		Content:   content,
		Provider:  "",
		Model:     "",
	}
	entry, err := store.AppendMessage(ctx, sessionID, parentID, &message)
	require.NoError(t, err)

	return entry
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
