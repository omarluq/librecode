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
	entryID := reconciliationFirstEntry
	local.EntryID = &entryID
	app.appendMessage(local)

	app.appendMissingSessionMessages([]database.SessionMessageEntity{
		testSessionMessage(local.CreatedAt.Add(time.Second), reconciliationFirstEntry),
	})

	require.Len(t, app.transcript.History, 1)
	require.NotNil(t, app.transcript.History[0].EntryID)
	assert.Equal(t, reconciliationFirstEntry, *app.transcript.History[0].EntryID)
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
	require.NotNil(t, app.transcript.History[0].EntryID)
	require.NotNil(t, app.transcript.History[1].EntryID)
	assert.Equal(t, []string{reconciliationFirstEntry, reconciliationSecondEntry}, []string{
		*app.transcript.History[0].EntryID,
		*app.transcript.History[1].EntryID,
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

func TestBindPromptUserMessageEntryIDTargetsTrackedMessage(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.appendMessage(newChatMessage(transcript.RoleUser, reconciliationContent))
	app.appendMessage(newChatMessage(transcript.RoleUser, reconciliationContent))
	app.activePrompt = newTestActivePrompt(nil)
	app.activePrompt.UserMessageTimestamp = app.transcript.History[1].CreatedAt.UnixNano()

	app.bindPromptUserMessageEntryID(reconciliationSecondEntry)

	assert.Nil(t, app.transcript.History[0].EntryID)
	require.NotNil(t, app.transcript.History[1].EntryID)
	assert.Equal(t, reconciliationSecondEntry, *app.transcript.History[1].EntryID)
}

func testSessionMessage(createdAt time.Time, entryID string) database.SessionMessageEntity {
	return database.SessionMessageEntity{
		CreatedAt: createdAt,
		ID:        "message-" + entryID,
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
