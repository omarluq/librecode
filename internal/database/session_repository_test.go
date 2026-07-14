package database_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite" // register sqlite driver for repository integration tests

	"github.com/omarluq/librecode/internal/database"
)

const (
	testHello              = "hello"
	testVisible            = "visible"
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
	assertUUIDV7(t, createdSession.ID)

	firstMessage := database.MessageEntity{
		Timestamp: time.Now().UTC(),
		Role:      database.RoleUser,
		Content:   testHello,
		Provider:  "",
		Model:     "",
	}
	firstEntry, err := repository.AppendMessage(ctx, createdSession.ID, nil, &firstMessage)
	require.NoError(t, err)
	assertUUIDV7(t, firstEntry.ID)

	secondMessage := database.MessageEntity{
		Timestamp: time.Now().UTC(),
		Role:      database.RoleAssistant,
		Content:   "hi",
		Provider:  "local",
		Model:     "librecode",
	}
	secondEntry, err := repository.AppendMessage(ctx, createdSession.ID, &firstEntry.ID, &secondMessage)
	require.NoError(t, err)
	assertUUIDV7(t, secondEntry.ID)

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

	rootChildren, err := repository.Children(ctx, createdSession.ID, nil)
	require.NoError(t, err)
	require.Len(t, rootChildren, 1)
	assert.Equal(t, firstEntry.ID, rootChildren[0].ID)
	childEntries, err := repository.Children(ctx, createdSession.ID, &firstEntry.ID)
	require.NoError(t, err)
	require.Len(t, childEntries, 1)
	assert.Equal(t, secondEntry.ID, childEntries[0].ID)

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

func TestSessionRepository_TranscriptMessagesHonorsDisplayMetadata(t *testing.T) {
	t.Parallel()

	repository := newTestSessionRepository(t)
	ctx := context.Background()
	session, err := repository.CreateSession(ctx, "/work", "transcript", "")
	require.NoError(t, err)

	visible := true
	hidden := false
	modelFacing := true
	first, err := repository.AppendMessageWithDisplay(ctx, session.ID, nil, &database.MessageEntity{
		Timestamp: time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC),
		Role:      database.RoleUser,
		Content:   testVisible,
		Provider:  "",
		Model:     "",
	}, &modelFacing, &visible)
	require.NoError(t, err)
	second, err := repository.AppendMessageWithDisplay(ctx, session.ID, &first.ID, &database.MessageEntity{
		Timestamp: time.Date(2026, time.January, 2, 3, 4, 6, 0, time.UTC),
		Role:      database.RoleAssistant,
		Content:   "hidden",
		Provider:  "",
		Model:     "",
	}, &modelFacing, &hidden)
	require.NoError(t, err)
	_, err = repository.AppendMessage(ctx, session.ID, &second.ID, &database.MessageEntity{
		Timestamp: time.Date(2026, time.January, 2, 3, 4, 7, 0, time.UTC),
		Role:      database.RoleAssistant,
		Content:   "default visible",
		Provider:  "",
		Model:     "",
	})
	require.NoError(t, err)

	all, err := repository.Messages(ctx, session.ID)
	require.NoError(t, err)
	require.Len(t, all, 3)

	transcript, err := repository.TranscriptMessages(ctx, session.ID)
	require.NoError(t, err)
	require.Len(t, transcript, 2)
	assert.Equal(t, []string{testVisible, "default visible"}, []string{transcript[0].Content, transcript[1].Content})

	entries, err := repository.Entries(ctx, session.ID)
	require.NoError(t, err)
	require.Len(t, entries, 3)
	assert.True(t, entries[0].Display)
	assert.False(t, entries[1].Display)
	assert.True(t, entries[2].Display)
}

func TestSessionRepository_AppendMessageWithDisplayDefaultsIndependently(t *testing.T) {
	t.Parallel()

	repository := newTestSessionRepository(t)
	ctx := context.Background()
	session, err := repository.CreateSession(ctx, "/work", "display defaults", "")
	require.NoError(t, err)

	hidden := false
	entry, err := repository.AppendMessageWithDisplay(ctx, session.ID, nil, &database.MessageEntity{
		Timestamp: time.Time{}, Role: database.RoleUser, Content: "hidden but model-facing by role default",
		Provider: "", Model: "",
	}, nil, &hidden)
	require.NoError(t, err)
	assert.False(t, entry.Display)
	assert.True(t, entry.ModelFacing)

	visible := true
	entry, err = repository.AppendMessageWithDisplay(ctx, session.ID, &entry.ID, &database.MessageEntity{
		Timestamp: time.Time{}, Role: database.RoleAssistant, Content: "shown",
		Provider: "", Model: "",
	}, nil, &visible)
	require.NoError(t, err)
	assert.True(t, entry.Display)
	assert.True(t, entry.ModelFacing)
}

func TestSessionRepository_TranscriptMessagesWrapsMalformedRows(t *testing.T) {
	t.Parallel()

	connection, err := sql.Open(sqliteDriver(), ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	connection.SetMaxOpenConns(1)
	require.NoError(t, database.Migrate(context.Background(), connection))

	repository := database.NewSessionRepository(connection)
	session, err := repository.CreateSession(context.Background(), "/work", "malformed transcript", "")
	require.NoError(t, err)
	_, err = repository.AppendMessage(context.Background(), session.ID, nil, &database.MessageEntity{
		Timestamp: time.Time{}, Role: database.RoleUser, Content: "malformed",
		Provider: "", Model: "",
	})
	require.NoError(t, err)
	_, err = connection.ExecContext(context.Background(), `UPDATE session_messages SET created_at = 'invalid'`)
	require.NoError(t, err)

	messages, err := repository.TranscriptMessages(context.Background(), session.ID)
	require.ErrorContains(t, err, "scan transcript messages")
	assert.Nil(t, messages)
}

func TestSessionRepository_TranscriptMessagesReturnsContextError(t *testing.T) {
	t.Parallel()

	repository := newTestSessionRepository(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	messages, err := repository.TranscriptMessages(ctx, "session")
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, messages)
}

func TestSessionRepository_AppendMessagePreservesInputTimestamp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		timestamp time.Time
		name      string
	}{
		{
			name:      "UTC timestamp",
			timestamp: time.Date(2026, time.May, 31, 12, 34, 56, 789000000, time.UTC),
		},
		{
			name:      "non-UTC timestamp",
			timestamp: time.Date(2026, time.May, 31, 7, 34, 56, 789000000, time.FixedZone("CDT", -5*60*60)),
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			repository := newTestSessionRepository(t)
			ctx := context.Background()

			createdSession, err := repository.CreateSession(ctx, "/work", "timestamp", "")
			require.NoError(t, err)

			message := database.MessageEntity{
				Timestamp: testCase.timestamp,
				Role:      database.RoleUser,
				Content:   testHello,
				Provider:  "",
				Model:     "",
			}
			expectedTimestamp := testCase.timestamp.UTC()

			entry, err := repository.AppendMessage(ctx, createdSession.ID, nil, &message)
			require.NoError(t, err)
			assert.Equal(t, expectedTimestamp, entry.CreatedAt)
			assert.Equal(t, expectedTimestamp, entry.Message.Timestamp)

			storedMessage, found, err := repository.MessageForEntry(ctx, createdSession.ID, entry.ID)
			require.NoError(t, err)
			require.True(t, found)
			assert.Equal(t, expectedTimestamp, storedMessage.CreatedAt)
		})
	}
}

func TestSessionRepository_LoadsAndListsSessions(t *testing.T) {
	t.Parallel()

	repository := newTestSessionRepository(t)
	ctx := context.Background()

	oldSession, err := repository.CreateSession(ctx, "/work", "old", "")
	require.NoError(t, err)
	newSession, err := repository.CreateSession(ctx, "/work", "new", oldSession.ID)
	require.NoError(t, err)
	_, err = repository.CreateSession(ctx, "/other", "other", "")
	require.NoError(t, err)

	foundSession, found, err := repository.GetSession(ctx, newSession.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, oldSession.ID, foundSession.ParentSession)

	sessions, err := repository.ListSessions(ctx, "/work")
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, oldSession.ID, sessions[0].ID)

	children, err := repository.ListChildSessions(ctx, oldSession.ID)
	require.NoError(t, err)
	require.Len(t, children, 1)
	assert.Equal(t, newSession.ID, children[0].ID)

	latest, found, err := repository.LatestSession(ctx, "/work")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, oldSession.ID, latest.ID)
}

func TestSessionRepository_DeleteSessionRemovesSessionRows(t *testing.T) {
	t.Parallel()

	repository := newTestSessionRepository(t)
	ctx := context.Background()
	createdSession, err := repository.CreateSession(ctx, "/work", "delete-me", "")
	require.NoError(t, err)
	helper := sessionTestHelper{ctx, t, repository}
	entry := helper.appendMessage(createdSession.ID, nil, database.RoleUser, testHello)

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
	helper := sessionTestHelper{ctx, t, repository}
	rootEntry := helper.appendMessage(createdSession.ID, nil, database.RoleUser, "root")
	branchEntry := helper.appendMessage(createdSession.ID, &rootEntry.ID, database.RoleUser, "branch")
	childEntry := helper.appendMessage(
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

func TestSessionRepository_BranchLoadsOnlyActiveChain(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := newTestSessionRepository(t)
	session, err := repository.CreateSession(ctx, "/work", "", "")
	require.NoError(t, err)

	helper := sessionTestHelper{ctx, t, repository}
	root := helper.appendMessage(session.ID, nil, database.RoleUser, "root")
	child := helper.appendMessage(session.ID, &root.ID, database.RoleAssistant, "child")
	grandchild := helper.appendMessage(session.ID, &child.ID, database.RoleUser, "grandchild")

	// Sibling branch off root — should NOT appear in the active chain.
	sibling := helper.appendMessage(session.ID, &root.ID, database.RoleAssistant, "sibling")

	// Explicit entry ID returns root-to-entry chain, excluding siblings.
	branch, err := repository.Branch(ctx, session.ID, grandchild.ID)
	require.NoError(t, err)
	require.Len(t, branch, 3)
	assert.Equal(t, root.ID, branch[0].ID)
	assert.Equal(t, child.ID, branch[1].ID)
	assert.Equal(t, grandchild.ID, branch[2].ID)

	// Empty entry ID auto-resolves to the latest leaf (sibling, created last).
	branch, err = repository.Branch(ctx, session.ID, "")
	require.NoError(t, err)
	require.Len(t, branch, 2)
	assert.Equal(t, root.ID, branch[0].ID)
	assert.Equal(t, sibling.ID, branch[1].ID)

	// Grandchild must not appear in the sibling's chain.
	for index := range branch {
		assert.NotEqual(t, grandchild.ID, branch[index].ID, "grandchild leaked into sibling chain")
	}
}

func TestSessionRepository_BranchReturnsErrorForMissingEntryID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := newTestSessionRepository(t)
	session, err := repository.CreateSession(ctx, "/work", "", "")
	require.NoError(t, err)

	_, err = repository.Branch(ctx, session.ID, "nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestSessionRepository_BranchResolvesLeafNotLatestByTimestamp(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := newTestSessionRepository(t)
	session, err := repository.CreateSession(ctx, "/work", "", "")
	require.NoError(t, err)

	helper := sessionTestHelper{ctx, t, repository}

	// root has two children: "child" (a leaf, created first) and
	// "backdated-parent" (a non-leaf with a child of its own).
	root := helper.appendMessage(session.ID, nil, database.RoleUser, "root")
	leafA := helper.appendMessage(session.ID, &root.ID, database.RoleAssistant, "leaf-a")

	// This entry is a non-leaf (has a child) but is backdated to be older
	// than leafA. The old ORDER BY created_at logic would have picked leafA
	// as the starting point since it's newer. The leaf-aware query must
	// select the actual leaf with no children.
	backdatedParent := helper.appendMessageAt(
		session.ID, &root.ID, database.RoleAssistant, "backdated-parent",
		time.Now().Add(-1*time.Hour),
	)
	backdatedLeaf := helper.appendMessage(
		session.ID, &backdatedParent.ID, database.RoleUser, "backdated-leaf",
	)

	// Empty entryID must resolve to an actual leaf. There are two leaves:
	// leafA and backdatedLeaf. The most recently created leaf is
	// backdatedLeaf (created last even though its parent is backdated).
	branch, err := repository.Branch(ctx, session.ID, "")
	require.NoError(t, err)
	require.Len(t, branch, 3)
	assert.Equal(t, root.ID, branch[0].ID)
	assert.Equal(t, backdatedParent.ID, branch[1].ID)
	assert.Equal(t, backdatedLeaf.ID, branch[2].ID)

	// leafA must NOT appear — it's not on this branch.
	for index := range branch {
		assert.NotEqual(t, leafA.ID, branch[index].ID, "leaf-a leaked into backdated branch")
	}
}

func TestSessionRepository_SupportsLibrecodeStyleTreeMetadata(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := newMetadataFixture(ctx, t)

	foundLabel, found, err := fixture.repository.Label(ctx, fixture.sessionID, fixture.userEntry.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, fixture.label, foundLabel)

	branch, err := fixture.repository.Branch(ctx, fixture.sessionID, fixture.branchEntry.ID)
	require.NoError(t, err)
	require.Len(t, branch, 2)
	assert.Equal(t, fixture.userEntry.ID, branch[0].ID)
	assert.Equal(t, fixture.branchEntry.ID, branch[1].ID)

	tree, err := fixture.repository.Tree(ctx, fixture.sessionID)
	require.NoError(t, err)
	require.Len(t, tree, 1)
	assert.Equal(t, fixture.userEntry.ID, tree[0].Entry.ID)
	require.Len(t, tree[0].Children, 2)

	customMessage, found, err := fixture.repository.MessageForEntry(
		ctx,
		fixture.sessionID,
		fixture.customMessageEntry.ID,
	)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, testCustomMessageType, customMessage.Sender)
	assert.Equal(t, database.RoleCustom, customMessage.Role)
	assert.Equal(t, testCustomMessageValue, customMessage.Content)

	contextEntity, err := fixture.repository.BuildContext(ctx, fixture.sessionID, fixture.compactionEntry.ID)
	require.NoError(t, err)
	assert.Equal(t, "anthropic", contextEntity.Provider)
	assert.Equal(t, "sonnet", contextEntity.Model)
	assert.Equal(t, "high", contextEntity.ThinkingLevel)
	require.Len(t, contextEntity.Messages, 3)
	assert.Equal(t, database.RoleCompactionSummary, contextEntity.Messages[0].Role)
	assert.Equal(t, "summary of earlier work", contextEntity.Messages[0].Content)
	assert.Equal(t, database.RoleAssistant, contextEntity.Messages[1].Role)
	assert.Equal(t, testAssistantReply, contextEntity.Messages[1].Content)
	assert.Equal(t, database.RoleCustom, contextEntity.Messages[2].Role)
	assert.Equal(t, testCustomMessageValue, contextEntity.Messages[2].Content)

	_, err = fixture.repository.AppendSessionInfo(
		ctx,
		fixture.sessionID,
		&fixture.compactionEntry.ID,
		"named session",
	)
	require.NoError(t, err)
	updatedSession, found, err := fixture.repository.GetSession(ctx, fixture.sessionID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "named session", updatedSession.Name)
}

type metadataFixture struct {
	repository         *database.SessionRepository
	userEntry          *database.EntryEntity
	branchEntry        *database.EntryEntity
	customMessageEntry *database.EntryEntity
	compactionEntry    *database.EntryEntity
	sessionID          string
	label              string
}

func newMetadataFixture(ctx context.Context, t *testing.T) metadataFixture {
	t.Helper()

	repository := newTestSessionRepository(t)
	createdSession, err := repository.CreateSession(ctx, "/work", "", "")
	require.NoError(t, err)

	helper := sessionTestHelper{ctx, t, repository}
	userEntry := helper.appendMessage(createdSession.ID, nil, database.RoleUser, testUserPrompt)
	modelEntry, err := repository.AppendModelChange(ctx, createdSession.ID, &userEntry.ID, "anthropic", "sonnet")
	require.NoError(t, err)
	thinkingEntry, err := repository.AppendThinkingLevelChange(ctx, createdSession.ID, &modelEntry.ID, "high")
	require.NoError(t, err)

	assistantEntry := helper.appendMessage(
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

	compactionEntry := helper.appendCompactionSimple(
		createdSession.ID, &customEntry.ID,
		"summary of earlier work", assistantEntry.ID, 1200,
	)
	branchEntry := helper.appendBranchSummary(
		createdSession.ID, &userEntry.ID,
		compactionEntry.ID, "branch",
	)

	return metadataFixture{
		repository:         repository,
		userEntry:          userEntry,
		branchEntry:        branchEntry,
		customMessageEntry: customEntry,
		compactionEntry:    compactionEntry,
		sessionID:          createdSession.ID,
		label:              label,
	}
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

func assertUUIDV7(t *testing.T, value string) {
	t.Helper()

	parsed, err := uuid.FromString(value)
	require.NoError(t, err)
	assert.Equal(t, byte(7), parsed.Version())
}

func TestSessionRepository_AppendCustomAndCustomEntry(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := newTestSessionRepository(t)
	session, err := repository.CreateSession(ctx, "/work", "", "")
	require.NoError(t, err)

	helper := sessionTestHelper{ctx, t, repository}
	root := helper.appendMessage(session.ID, nil, database.RoleUser, "root")

	// AppendCustom delegates to AppendCustomEntry with nil parent.
	customEntry, err := repository.AppendCustom(ctx, session.ID, "note", `{"key":"value"}`)
	require.NoError(t, err)
	assert.Equal(t, database.EntryTypeCustom, customEntry.Type)
	assert.Equal(t, "note", customEntry.CustomType)
	assert.Nil(t, customEntry.ParentID)

	// AppendCustomEntry with explicit parent.
	customChild, err := repository.AppendCustomEntry(ctx, session.ID, &root.ID, "checkpoint", `{"step":1}`)
	require.NoError(t, err)
	assert.Equal(t, database.EntryTypeCustom, customChild.Type)
	assert.Equal(t, "checkpoint", customChild.CustomType)
	require.NotNil(t, customChild.ParentID)
	assert.Equal(t, root.ID, *customChild.ParentID)
}

func TestSessionRepository_BuildContextIncludesBranchSummary(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := newTestSessionRepository(t)
	session, err := repository.CreateSession(ctx, "/work", "", "")
	require.NoError(t, err)

	helper := sessionTestHelper{ctx, t, repository}
	root := helper.appendMessage(session.ID, nil, database.RoleUser, "root")
	branchSummary := helper.appendBranchSummary(
		session.ID, &root.ID,
		root.ID, "work from the other branch",
	)

	contextEntity, err := repository.BuildContext(ctx, session.ID, branchSummary.ID)
	require.NoError(t, err)
	require.Len(t, contextEntity.Messages, 2)
	assert.Equal(t, database.RoleUser, contextEntity.Messages[0].Role)
	assert.Equal(t, database.RoleBranchSummary, contextEntity.Messages[1].Role)
	assert.Equal(t, "work from the other branch", contextEntity.Messages[1].Content)
}

func TestSessionRepository_BuildContextWithCompactionTailBranchSummary(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := newTestSessionRepository(t)
	session, err := repository.CreateSession(ctx, "/work", "", "")
	require.NoError(t, err)

	require.NoError(t, err)

	helper := sessionTestHelper{ctx, t, repository}
	root := helper.appendMessage(session.ID, nil, database.RoleUser, "root")
	branchSummary := helper.appendBranchSummary(
		session.ID, &root.ID,
		root.ID, "prior branch work",
	)

	compactionEntry := helper.appendCompactionSimple(
		session.ID, &branchSummary.ID,
		"compacted", root.ID, 500,
	)

	// BuildContext from compaction: summary + tail (root + branchSummary).
	contextEntity, err := repository.BuildContext(ctx, session.ID, compactionEntry.ID)
	require.NoError(t, err)
	require.Len(t, contextEntity.Messages, 3)
	assert.Equal(t, database.RoleCompactionSummary, contextEntity.Messages[0].Role)
	assert.Equal(t, database.RoleUser, contextEntity.Messages[1].Role)
	assert.Equal(t, database.RoleBranchSummary, contextEntity.Messages[2].Role)
	assert.Equal(t, "prior branch work", contextEntity.Messages[2].Content)
}

func TestSessionRepository_LabelClearsWhenNil(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := newTestSessionRepository(t)
	session, err := repository.CreateSession(ctx, "/work", "", "")
	require.NoError(t, err)

	helper := sessionTestHelper{ctx, t, repository}
	root := helper.appendMessage(session.ID, nil, database.RoleUser, "root")

	// Set a label.
	label := "checkpoint"
	_, err = repository.AppendLabelChange(ctx, session.ID, &root.ID, root.ID, &label)
	require.NoError(t, err)
	foundLabel, found, err := repository.Label(ctx, session.ID, root.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "checkpoint", foundLabel)

	// Clear the label by passing nil.
	_, err = repository.AppendLabelChange(ctx, session.ID, &root.ID, root.ID, nil)
	require.NoError(t, err)
	clearedLabel, found, err := repository.Label(ctx, session.ID, root.ID)
	require.NoError(t, err)
	assert.False(t, found)
	assert.Empty(t, clearedLabel)
}

func sqliteDriver() string {
	return "sqlite"
}
