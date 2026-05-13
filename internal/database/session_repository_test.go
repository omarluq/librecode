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

const (
	testUserPrompt         = "pick a path"
	testAssistantReply     = "path A"
	testCustomMessageType  = "test-extension"
	testCustomMessageValue = "extra"
)

func TestSessionRepository_AppendsMessagesInSessionTree(t *testing.T) {
	t.Parallel()

	repository := newTestSessionRepository(t)
	ctx := context.Background()

	createdSession, err := repository.CreateSession(ctx, "/work", "port", "")
	require.NoError(t, err)

	firstMessage := database.MessageEntity{
		Timestamp: time.Now().UTC(),
		Role:      database.RoleUser,
		Content:   "hello",
		Provider:  "",
		Model:     "",
	}
	firstEntry, err := repository.AppendMessage(ctx, createdSession.ID, nil, &firstMessage)
	require.NoError(t, err)

	secondMessage := database.MessageEntity{
		Timestamp: time.Now().UTC(),
		Role:      database.RoleAssistant,
		Content:   "hi",
		Provider:  "local",
		Model:     "librecode",
	}
	secondEntry, err := repository.AppendMessage(ctx, createdSession.ID, &firstEntry.ID, &secondMessage)
	require.NoError(t, err)

	latestSession, found, err := repository.LatestSession(ctx, "/work")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, createdSession.ID, latestSession.ID)

	leafEntry, found, err := repository.LeafEntry(ctx, createdSession.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, secondEntry.ID, leafEntry.ID)

	entries, err := repository.Entries(ctx, createdSession.ID)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Nil(t, entries[0].ParentID)
	require.NotNil(t, entries[1].ParentID)
	assert.Equal(t, firstEntry.ID, *entries[1].ParentID)

	messages, err := repository.Messages(ctx, createdSession.ID)
	require.NoError(t, err)
	require.Len(t, messages, 2)
	assert.Equal(t, firstEntry.ID, messages[0].EntryID)
	assert.Equal(t, string(database.RoleUser), messages[0].Sender)
	assert.Equal(t, database.RoleUser, messages[0].Role)
	assert.Equal(t, secondEntry.ID, messages[1].EntryID)
	assert.Equal(t, string(database.RoleAssistant), messages[1].Sender)
	assert.Equal(t, database.RoleAssistant, messages[1].Role)

	message, found, err := repository.MessageForEntry(ctx, createdSession.ID, secondEntry.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "local", message.Provider)
	assert.Equal(t, "librecode", message.Model)
}

func TestSessionRepository_DeleteSessionRemovesSessionRows(t *testing.T) {
	t.Parallel()

	repository := newTestSessionRepository(t)
	ctx := context.Background()
	createdSession, err := repository.CreateSession(ctx, "/work", "delete-me", "")
	require.NoError(t, err)
	entry := appendTestMessage(ctx, t, repository, createdSession.ID, nil, database.RoleUser, "hello")

	require.NoError(t, repository.DeleteSession(ctx, createdSession.ID))

	_, found, err := repository.GetSession(ctx, createdSession.ID)
	require.NoError(t, err)
	assert.False(t, found)
	_, found, err = repository.Entry(ctx, createdSession.ID, entry.ID)
	require.NoError(t, err)
	assert.False(t, found)
	_, found, err = repository.MessageForEntry(ctx, createdSession.ID, entry.ID)
	require.NoError(t, err)
	assert.False(t, found)
}

func TestSessionRepository_DeleteEntryBranchRemovesDescendants(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := newTestSessionRepository(t)
	createdSession, err := repository.CreateSession(ctx, "/work", "", "")
	require.NoError(t, err)
	rootEntry := appendTestMessage(ctx, t, repository, createdSession.ID, nil, database.RoleUser, "root")
	branchEntry := appendTestMessage(ctx, t, repository, createdSession.ID, &rootEntry.ID, database.RoleUser, "branch")
	childEntry := appendTestMessage(
		ctx,
		t,
		repository,
		createdSession.ID,
		&branchEntry.ID,
		database.RoleAssistant,
		"child",
	)

	require.NoError(t, repository.DeleteEntryBranch(ctx, createdSession.ID, branchEntry.ID))

	_, found, err := repository.Entry(ctx, createdSession.ID, rootEntry.ID)
	require.NoError(t, err)
	assert.True(t, found)
	_, found, err = repository.Entry(ctx, createdSession.ID, branchEntry.ID)
	require.NoError(t, err)
	assert.False(t, found)
	_, found, err = repository.MessageForEntry(ctx, createdSession.ID, childEntry.ID)
	require.NoError(t, err)
	assert.False(t, found)
}

func TestSessionRepository_SupportsLibrecodeStyleTreeMetadata(t *testing.T) {
	t.Parallel()

	fixture := newMetadataFixture(t)

	foundLabel, found, err := fixture.repository.Label(fixture.ctx, fixture.sessionID, fixture.userEntry.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, fixture.label, foundLabel)

	branch, err := fixture.repository.Branch(fixture.ctx, fixture.sessionID, fixture.branchEntry.ID)
	require.NoError(t, err)
	require.Len(t, branch, 2)
	assert.Equal(t, fixture.userEntry.ID, branch[0].ID)
	assert.Equal(t, fixture.branchEntry.ID, branch[1].ID)

	tree, err := fixture.repository.Tree(fixture.ctx, fixture.sessionID)
	require.NoError(t, err)
	require.Len(t, tree, 1)
	assert.Equal(t, fixture.userEntry.ID, tree[0].Entry.ID)
	require.Len(t, tree[0].Children, 2)

	customMessage, found, err := fixture.repository.MessageForEntry(
		fixture.ctx,
		fixture.sessionID,
		fixture.customMessageEntry.ID,
	)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, testCustomMessageType, customMessage.Sender)
	assert.Equal(t, database.RoleCustom, customMessage.Role)
	assert.Equal(t, testCustomMessageValue, customMessage.Content)

	contextEntity, err := fixture.repository.BuildContext(fixture.ctx, fixture.sessionID, fixture.compactionEntry.ID)
	require.NoError(t, err)
	assert.Equal(t, "anthropic", contextEntity.Provider)
	assert.Equal(t, "sonnet", contextEntity.Model)
	assert.Equal(t, "high", contextEntity.ThinkingLevel)
	require.Len(t, contextEntity.Messages, 1)
	assert.Equal(t, database.RoleCompactionSummary, contextEntity.Messages[0].Role)
	assert.Equal(t, "summary of earlier work", contextEntity.Messages[0].Content)

	_, err = fixture.repository.AppendSessionInfo(
		fixture.ctx,
		fixture.sessionID,
		&fixture.compactionEntry.ID,
		"named session",
	)
	require.NoError(t, err)
	updatedSession, found, err := fixture.repository.GetSession(fixture.ctx, fixture.sessionID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "named session", updatedSession.Name)
}

type metadataFixture struct {
	ctx                context.Context
	repository         *database.SessionRepository
	userEntry          *database.EntryEntity
	branchEntry        *database.EntryEntity
	customMessageEntry *database.EntryEntity
	compactionEntry    *database.EntryEntity
	sessionID          string
	label              string
}

func newMetadataFixture(t *testing.T) metadataFixture {
	t.Helper()

	repository := newTestSessionRepository(t)
	ctx := context.Background()
	createdSession, err := repository.CreateSession(ctx, "/work", "", "")
	require.NoError(t, err)

	userEntry := appendTestMessage(ctx, t, repository, createdSession.ID, nil, database.RoleUser, testUserPrompt)
	modelEntry, err := repository.AppendModelChange(ctx, createdSession.ID, &userEntry.ID, "anthropic", "sonnet")
	require.NoError(t, err)
	thinkingEntry, err := repository.AppendThinkingLevelChange(ctx, createdSession.ID, &modelEntry.ID, "high")
	require.NoError(t, err)
	assistantEntry := appendTestMessage(
		ctx,
		t,
		repository,
		createdSession.ID,
		&thinkingEntry.ID,
		database.RoleAssistant,
		testAssistantReply,
	)

	label := "checkpoint"
	labelEntry, err := repository.AppendLabelChange(ctx, createdSession.ID, &assistantEntry.ID, userEntry.ID, &label)
	require.NoError(t, err)
	customEntry, err := repository.AppendCustomMessage(
		ctx,
		createdSession.ID,
		&labelEntry.ID,
		testCustomMessageType,
		testCustomMessageValue,
		true,
		nil,
	)
	require.NoError(t, err)
	compactionEntry, err := repository.AppendCompaction(
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
	branchEntry, err := repository.AppendBranchSummary(
		ctx,
		createdSession.ID,
		&userEntry.ID,
		compactionEntry.ID,
		"branch",
		nil,
		false,
	)
	require.NoError(t, err)

	return metadataFixture{
		ctx:                ctx,
		repository:         repository,
		userEntry:          userEntry,
		branchEntry:        branchEntry,
		customMessageEntry: customEntry,
		compactionEntry:    compactionEntry,
		sessionID:          createdSession.ID,
		label:              label,
	}
}

func appendTestMessage(
	ctx context.Context,
	t *testing.T,
	repository *database.SessionRepository,
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
	entry, err := repository.AppendMessage(ctx, sessionID, parentID, &message)
	require.NoError(t, err)

	return entry
}

func newTestSessionRepository(t *testing.T) *database.SessionRepository {
	t.Helper()

	connection, err := sql.Open(sqliteDriver(), ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, connection.Close())
	})
	connection.SetMaxOpenConns(1)

	require.NoError(t, database.Migrate(context.Background(), connection))

	return database.NewSessionRepository(connection)
}

func newMigratedThroughVersion(t *testing.T, version int64) *sql.DB {
	t.Helper()

	connection, err := sql.Open(sqliteDriver(), ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, connection.Close())
	})
	connection.SetMaxOpenConns(1)

	migrationRoot, err := database.MigrationFS()
	require.NoError(t, err)
	provider, err := database.NewMigrationProvider(connection, migrationRoot)
	require.NoError(t, err)
	_, err = provider.UpTo(context.Background(), version)
	require.NoError(t, err)

	return connection
}

func sqliteDriver() string {
	return "sqlite"
}
