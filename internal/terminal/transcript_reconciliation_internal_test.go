package terminal

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/transcript"
)

const (
	reconciliationFirstEntry  = "reconciliation-first-entry"
	reconciliationSecondEntry = "reconciliation-second-entry"
	reconciliationContent     = "reconciliation-repeat"
)

func TestAppendMissingSessionMessagesReconcilesByEntryID(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	local := newChatMessage(transcript.RoleUser, reconciliationContent)
	local.Identity = &chatMessageIdentity{EntryID: reconciliationFirstEntry, PromptID: 0}
	app.appendMessage(local)

	app.appendMissingSessionMessages([]database.SessionMessageEntity{
		testSessionMessage(local.CreatedAt.Add(time.Second), reconciliationFirstEntry),
	})

	require.Len(t, app.transcript.History, 1)
	assert.Equal(t, reconciliationFirstEntry, app.transcript.History[0].Identity.EntryID)
}

func TestAppendMissingSessionMessagesPreservesRepeatedContentWithDistinctEntryIDs(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	createdAt := time.Now().UTC()
	app.appendSessionMessages([]database.SessionMessageEntity{
		testSessionMessage(createdAt, reconciliationFirstEntry),
	})

	app.appendMissingSessionMessages([]database.SessionMessageEntity{
		testSessionMessage(createdAt, reconciliationFirstEntry),
		testSessionMessage(createdAt, reconciliationSecondEntry),
	})

	require.Len(t, app.transcript.History, 2)
	assert.Equal(t, []string{reconciliationFirstEntry, reconciliationSecondEntry}, []string{
		app.transcript.History[0].Identity.EntryID,
		app.transcript.History[1].Identity.EntryID,
	})
}

func TestAppendMissingSessionMessagesDoesNotReconcileDurableEntryByTimestampAndContent(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	createdAt := time.Now().UTC()
	local := newChatMessage(transcript.RoleUser, reconciliationContent)
	local.CreatedAt = createdAt
	local.Identity = &chatMessageIdentity{EntryID: "", PromptID: 1}
	app.appendMessage(local)

	app.appendMissingSessionMessages([]database.SessionMessageEntity{
		testSessionMessage(createdAt, reconciliationFirstEntry),
	})

	require.Len(t, app.transcript.History, 2)
	require.NotNil(t, app.transcript.History[0].Identity)
	assert.Empty(t, app.transcript.History[0].Identity.EntryID)
	assert.Equal(t, reconciliationFirstEntry, app.transcript.History[1].Identity.EntryID)
}

func TestAppendMissingSessionMessagesOrdersEqualTimestampDurableEntriesByID(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	createdAt := time.Now().UTC()
	app.appendMissingSessionMessages([]database.SessionMessageEntity{
		testSessionMessage(createdAt, reconciliationSecondEntry),
		testSessionMessage(createdAt, reconciliationFirstEntry),
	})

	require.Len(t, app.transcript.History, 2)
	assert.Equal(t, []string{reconciliationFirstEntry, reconciliationSecondEntry}, []string{
		app.transcript.History[0].Identity.EntryID,
		app.transcript.History[1].Identity.EntryID,
	})
}

func TestAppendMissingSessionMessagesPreservesEqualTimestampLocalInsertionOrder(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	createdAt := time.Now().UTC()
	first := newChatMessage(transcript.RoleUser, "first-local")
	first.CreatedAt = createdAt
	first.Identity = &chatMessageIdentity{EntryID: "", PromptID: 2}
	second := newChatMessage(transcript.RoleUser, "second-local")
	second.CreatedAt = createdAt
	second.Identity = &chatMessageIdentity{EntryID: "", PromptID: 1}

	app.appendMessage(first)
	app.appendMessage(second)

	app.appendMissingSessionMessages([]database.SessionMessageEntity{
		testSessionMessage(createdAt.Add(time.Second), reconciliationFirstEntry),
	})

	require.Len(t, app.transcript.History, 3)
	assert.Equal(t, []string{"first-local", "second-local"}, []string{
		app.transcript.History[0].Content,
		app.transcript.History[1].Content,
	})
}

func TestAppendMissingSessionMessagesIsIdempotent(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	message := testSessionMessage(time.Now().UTC(), reconciliationFirstEntry)

	app.appendMissingSessionMessages([]database.SessionMessageEntity{message})
	app.appendMissingSessionMessages([]database.SessionMessageEntity{message})

	require.Len(t, app.transcript.History, 1)
	assert.Equal(t, []string{reconciliationContent}, app.promptHistory)
}

func TestBindPromptUserMessageEntryIDTargetsLocalPromptIdentity(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	createdAt := time.Now().UTC()
	first := newChatMessage(transcript.RoleUser, reconciliationContent)
	first.CreatedAt = createdAt
	first.Identity = &chatMessageIdentity{EntryID: "", PromptID: 1}
	second := newChatMessage(transcript.RoleUser, reconciliationContent)
	second.CreatedAt = createdAt
	second.Identity = &chatMessageIdentity{EntryID: "", PromptID: 2}

	app.appendMessage(first)
	app.appendMessage(second)

	app.bindPromptUserMessageEntryID(first.Identity.PromptID, reconciliationFirstEntry)
	app.bindPromptUserMessageEntryID(second.Identity.PromptID, reconciliationSecondEntry)

	assert.Equal(t, reconciliationFirstEntry, app.transcript.History[0].Identity.EntryID)
	assert.Equal(t, reconciliationSecondEntry, app.transcript.History[1].Identity.EntryID)
	assert.True(t, app.transcript.HasOlder)
}

func testSessionMessage(createdAt time.Time, entryID string) database.SessionMessageEntity {
	return database.SessionMessageEntity{
		CreatedAt: createdAt,
		SessionID: "session",
		EntryID:   entryID,
		Sender:    "",
		Role:      database.RoleUser,
		Content:   reconciliationContent,
		Provider:  "",
		Model:     "",
		Parts:     nil,
	}
}
