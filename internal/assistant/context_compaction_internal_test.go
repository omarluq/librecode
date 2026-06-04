package assistant

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

//nolint:govet // Test fixture readability matters more than field packing.
type planCompactionCase struct {
	entries  []database.EntryEntity
	assertFn func(t *testing.T, plan *compactionPlan)
	name     string
	wantErr  string
	keep     int
}

func TestPlanCompactionScenarios(t *testing.T) {
	t.Parallel()

	for _, testCase := range planCompactionCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			plan, err := planCompaction(testCase.entries, testCase.keep)
			if testCase.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.wantErr)
				testCase.assertFn(t, &plan)

				return
			}
			require.NoError(t, err)
			testCase.assertFn(t, &plan)
		})
	}
}

func planCompactionCases() []planCompactionCase {
	return []planCompactionCase{
		previousKeptBoundaryCase(),
		turnBoundaryCase(),
		defaultKeepRecentTokensCase(),
		latestCompactionCase(),
	}
}

func previousKeptBoundaryCase() planCompactionCase {
	oldUser := compactMessageEntry("old-user", database.RoleUser, "old user history")
	oldAssistant := compactMessageEntry("old-assistant", database.RoleAssistant, "old assistant history")
	firstSummary := compactionTestEntry(
		"first-summary",
		database.EntryTypeCompaction,
		database.RoleCompactionSummary,
		"first compacted summary",
	)
	firstSummary.Summary = "first compacted summary"
	firstSummary.CompactionFirstKeptEntryID = oldAssistant.ID
	recentUser := compactMessageEntry("recent-user", database.RoleUser, "recent user tail")
	recentAssistant := compactMessageEntry("recent-assistant", database.RoleAssistant, "recent assistant tail")

	return planCompactionCase{
		assertFn: assertPreviousKeptBoundaryPlan,
		entries:  []database.EntryEntity{oldUser, oldAssistant, firstSummary, recentUser, recentAssistant},
		name:     "uses previous kept boundary",
		wantErr:  "",
		keep:     1,
	}
}

func assertPreviousKeptBoundaryPlan(t *testing.T, plan *compactionPlan) {
	t.Helper()

	assert.Equal(t, "first compacted summary", plan.PreviousSummary)
	assert.Equal(t, []string{"old-assistant", "recent-user"}, plan.SummarizedEntryIDs)
	assert.Equal(t, []string{"recent-assistant"}, plan.KeptEntryIDs)
	assert.Equal(t, "recent-assistant", plan.FirstKeptEntryID)
	require.Len(t, plan.Messages, 2)
	assert.Equal(t, "old assistant history", plan.Messages[0].Content)
	assert.Equal(t, "recent user tail", plan.Messages[1].Content)
}

func turnBoundaryCase() planCompactionCase {
	return planCompactionCase{
		assertFn: assertTurnBoundaryPlan,
		entries: []database.EntryEntity{
			compactMessageEntry("user-1", database.RoleUser, "first user"),
			compactMessageEntry("assistant-1", database.RoleAssistant, "first assistant"),
			compactMessageEntry("user-2", database.RoleUser, "second user"),
			compactMessageEntry("assistant-2", database.RoleAssistant, "second assistant long enough"),
		},
		name:    "cuts at turn boundary when possible",
		wantErr: "",
		keep:    8,
	}
}

func assertTurnBoundaryPlan(t *testing.T, plan *compactionPlan) {
	t.Helper()

	assert.Equal(t, []string{"user-1", "assistant-1"}, plan.SummarizedEntryIDs)
	assert.Equal(t, []string{"user-2", "assistant-2"}, plan.KeptEntryIDs)
	assert.Equal(t, "user-2", plan.FirstKeptEntryID)
}

func defaultKeepRecentTokensCase() planCompactionCase {
	return planCompactionCase{
		assertFn: assertDefaultKeepRecentTokensPlan,
		entries: []database.EntryEntity{
			compactMessageEntry("user-1", database.RoleUser, strings.Repeat("old ", 30_000)),
			compactMessageEntry("assistant-1", database.RoleAssistant, strings.Repeat("old ", 30_000)),
			compactMessageEntry("user-2", database.RoleUser, "second user"),
			compactMessageEntry("assistant-2", database.RoleAssistant, "second assistant"),
		},
		name:    "falls back to default keep recent tokens",
		wantErr: "",
		keep:    0,
	}
}

func assertDefaultKeepRecentTokensPlan(t *testing.T, plan *compactionPlan) {
	t.Helper()

	assert.NotEmpty(t, plan.SummarizedEntryIDs)
	assert.NotEmpty(t, plan.KeptEntryIDs)
}

func latestCompactionCase() planCompactionCase {
	firstUser := compactMessageEntry("user-1", database.RoleUser, "first user")
	latestSummary := compactionTestEntry(
		"summary",
		database.EntryTypeCompaction,
		database.RoleCompactionSummary,
		"already compacted",
	)
	latestSummary.CompactionFirstKeptEntryID = firstUser.ID

	return planCompactionCase{
		assertFn: assertLatestCompactionPlan,
		entries:  []database.EntryEntity{firstUser, latestSummary},
		name:     "rejects latest compaction",
		wantErr:  "no new history to compact",
		keep:     1,
	}
}

func assertLatestCompactionPlan(t *testing.T, plan *compactionPlan) {
	t.Helper()

	assert.Empty(t, plan.FirstKeptEntryID)
}

func TestCompactionSystemPromptIncludesPreviousSummary(t *testing.T) {
	t.Parallel()

	prompt := compactionSystemPrompt("previous compacted facts")

	assert.Contains(t, prompt, "Update the existing compaction summary")
	assert.Contains(t, prompt, "previous compacted facts")
}

func compactMessageEntry(entryID string, role database.Role, content string) database.EntryEntity {
	return compactionTestEntry(entryID, database.EntryTypeMessage, role, content)
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
