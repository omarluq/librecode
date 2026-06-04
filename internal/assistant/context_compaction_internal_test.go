package assistant

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

func TestPlanCompactionAfterPreviousCompactionUsesPreviousKeptBoundary(t *testing.T) {
	t.Parallel()

	oldUser := compactionTestEntry("old-user", database.EntryTypeMessage, database.RoleUser, "old user history")
	oldAssistant := compactionTestEntry(
		"old-assistant",
		database.EntryTypeMessage,
		database.RoleAssistant,
		"old assistant history",
	)
	firstSummary := compactionTestEntry(
		"first-summary",
		database.EntryTypeCompaction,
		database.RoleCompactionSummary,
		"first compacted summary",
	)
	firstSummary.Summary = "first compacted summary"
	firstSummary.CompactionFirstKeptEntryID = oldAssistant.ID
	recentUser := compactionTestEntry("recent-user", database.EntryTypeMessage, database.RoleUser, "recent user tail")
	recentAssistant := compactionTestEntry(
		"recent-assistant",
		database.EntryTypeMessage,
		database.RoleAssistant,
		"recent assistant tail",
	)

	plan, err := planCompaction(
		[]database.EntryEntity{oldUser, oldAssistant, firstSummary, recentUser, recentAssistant},
		1,
	)

	require.NoError(t, err)
	assert.Equal(t, "first compacted summary", plan.PreviousSummary)
	assert.Equal(t, []string{oldAssistant.ID, recentUser.ID}, plan.SummarizedEntryIDs)
	assert.Equal(t, []string{recentAssistant.ID}, plan.KeptEntryIDs)
	assert.Equal(t, recentAssistant.ID, plan.FirstKeptEntryID)
	require.Len(t, plan.Messages, 2)
	assert.Equal(t, "old assistant history", plan.Messages[0].Content)
	assert.Equal(t, "recent user tail", plan.Messages[1].Content)
}

func TestPlanCompactionCutsAtTurnBoundaryWhenPossible(t *testing.T) {
	t.Parallel()

	firstUser := compactionTestEntry("user-1", database.EntryTypeMessage, database.RoleUser, "first user")
	firstAssistant := compactionTestEntry(
		"assistant-1",
		database.EntryTypeMessage,
		database.RoleAssistant,
		"first assistant",
	)
	secondUser := compactionTestEntry("user-2", database.EntryTypeMessage, database.RoleUser, "second user")
	secondAssistant := compactionTestEntry(
		"assistant-2",
		database.EntryTypeMessage,
		database.RoleAssistant,
		"second assistant long enough",
	)

	plan, err := planCompaction(
		[]database.EntryEntity{firstUser, firstAssistant, secondUser, secondAssistant},
		8,
	)

	require.NoError(t, err)
	assert.Equal(t, []string{firstUser.ID, firstAssistant.ID}, plan.SummarizedEntryIDs)
	assert.Equal(t, []string{secondUser.ID, secondAssistant.ID}, plan.KeptEntryIDs)
	assert.Equal(t, secondUser.ID, plan.FirstKeptEntryID)
}

func compactionTestEntry(
	entryID string,
	entryType database.EntryType,
	role database.Role,
	content string,
) database.EntryEntity {
	entry := database.EntryEntity{
		CreatedAt: time.Now().UTC(),
		ParentID:  nil,
		Message: database.MessageEntity{
			Timestamp: time.Now().UTC(),
			Role:      role,
			Content:   content,
			Provider:  "",
			Model:     "",
		},
		Summary:                    "",
		ToolStatus:                 "",
		Type:                       entryType,
		CustomType:                 "",
		DataJSON:                   "{}",
		ID:                         entryID,
		ToolName:                   "",
		SessionID:                  "session",
		ToolArgsJSON:               "",
		BranchFromEntryID:          "",
		CompactionFirstKeptEntryID: "",
		CompactionTokensBefore:     0,
		TokenEstimate:              0,
		Display:                    true,
		ModelFacing:                true,
	}
	if entryType == database.EntryTypeCompaction {
		entry.Summary = content
	}

	return entry
}
