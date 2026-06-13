package compaction

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

type planCompactionCase struct {
	assertFn func(t *testing.T, plan *Plan)
	name     string
	wantErr  string
	entries  []database.EntryEntity
	keep     int
}

func TestPlanCompactionScenarios(t *testing.T) {
	t.Parallel()

	for _, testCase := range planCompactionCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			plan, err := PlanBranch(testCase.entries, testCase.keep, estimateTokens)
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
		splitTurnSummaryCase(),
		invalidRecentTailTokensCase(),
		latestCompactionCase(),
	}
}

func previousKeptBoundaryCase() planCompactionCase {
	oldUser := messageEntry("old-user", database.RoleUser, "old user history")
	oldAssistant := messageEntry("old-assistant", database.RoleAssistant, "old assistant history")
	firstSummary := testEntry(
		"first-summary",
		database.EntryTypeCompaction,
		database.RoleCompactionSummary,
		"first compacted summary",
	)
	firstSummary.Summary = "first compacted summary"
	firstSummary.CompactionFirstKeptEntryID = oldAssistant.ID
	recentUser := messageEntry("recent-user", database.RoleUser, "recent user tail")
	recentAssistant := messageEntry("recent-assistant", database.RoleAssistant, "recent assistant tail")

	return planCompactionCase{
		assertFn: assertPreviousKeptBoundaryPlan,
		entries:  []database.EntryEntity{oldUser, oldAssistant, firstSummary, recentUser, recentAssistant},
		name:     "uses previous kept boundary",
		wantErr:  "",
		keep:     1,
	}
}

func assertPreviousKeptBoundaryPlan(t *testing.T, plan *Plan) {
	t.Helper()

	assert.Equal(t, "first compacted summary", plan.PreviousSummary)
	assert.Equal(t, []string{"old-assistant", "recent-user"}, plan.SummarizedEntryIDs)
	assert.Equal(t, []string{"recent-assistant"}, plan.KeptEntryIDs)
	assert.Equal(t, "recent-assistant", plan.FirstKeptEntryID)
	require.Len(t, plan.Messages, 1)
	assert.Equal(t, "old assistant history", plan.Messages[0].Content)
	assert.Contains(t, plan.SplitTurnSummary, "recent user tail")
}

func turnBoundaryCase() planCompactionCase {
	return planCompactionCase{
		assertFn: assertTurnBoundaryPlan,
		entries: []database.EntryEntity{
			messageEntry("user-1", database.RoleUser, "first user"),
			messageEntry("assistant-1", database.RoleAssistant, "first assistant"),
			messageEntry("user-2", database.RoleUser, "second user"),
			messageEntry("assistant-2", database.RoleAssistant, "second assistant long enough"),
		},
		name:    "cuts at turn boundary when possible",
		wantErr: "",
		keep:    8,
	}
}

func assertTurnBoundaryPlan(t *testing.T, plan *Plan) {
	t.Helper()

	assert.Equal(t, []string{"user-1", "assistant-1"}, plan.SummarizedEntryIDs)
	assert.Equal(t, []string{"user-2", "assistant-2"}, plan.KeptEntryIDs)
	assert.Equal(t, "user-2", plan.FirstKeptEntryID)
}

func splitTurnSummaryCase() planCompactionCase {
	return planCompactionCase{
		assertFn: assertSplitTurnSummaryPlan,
		entries: []database.EntryEntity{
			messageEntry("user-1", database.RoleUser, strings.Repeat("old ", 10)),
			messageEntry("assistant-1", database.RoleAssistant, strings.Repeat("old ", 10)),
			messageEntry("user-2", database.RoleUser, "split user context"),
			messageEntry("assistant-2", database.RoleAssistant, strings.Repeat("large tail ", 100)),
		},
		name:    "records split turn summary separately",
		wantErr: "",
		keep:    40,
	}
}

func assertSplitTurnSummaryPlan(t *testing.T, plan *Plan) {
	t.Helper()

	assert.Equal(t, []string{"user-1", "assistant-1", "user-2"}, plan.SummarizedEntryIDs)
	assert.Equal(t, []string{"assistant-2"}, plan.KeptEntryIDs)
	assert.Equal(t, "assistant-2", plan.FirstKeptEntryID)
	assert.Contains(t, plan.SplitTurnSummary, "split user context")

	for index := range plan.Messages {
		assert.NotContains(t, plan.Messages[index].Content, "split user context")
	}
}

func invalidRecentTailTokensCase() planCompactionCase {
	return planCompactionCase{
		assertFn: assertNoCompactionPlan,
		entries: []database.EntryEntity{
			messageEntry("user-1", database.RoleUser, strings.Repeat("old ", 30_000)),
			messageEntry("assistant-1", database.RoleAssistant, strings.Repeat("old ", 30_000)),
			messageEntry("user-2", database.RoleUser, "second user"),
			messageEntry("assistant-2", database.RoleAssistant, "second assistant"),
		},
		name:    "rejects missing keep recent token target",
		wantErr: "recent tail token target must be greater than zero",
		keep:    0,
	}
}

func assertNoCompactionPlan(t *testing.T, plan *Plan) {
	t.Helper()

	assert.Empty(t, plan.FirstKeptEntryID)
}

func latestCompactionCase() planCompactionCase {
	firstUser := messageEntry("user-1", database.RoleUser, "first user")
	latestSummary := testEntry(
		"summary",
		database.EntryTypeCompaction,
		database.RoleCompactionSummary,
		"already compacted",
	)
	latestSummary.CompactionFirstKeptEntryID = firstUser.ID

	return planCompactionCase{
		assertFn: assertNoCompactionPlan,
		entries:  []database.EntryEntity{firstUser, latestSummary},
		name:     "rejects latest compaction",
		wantErr:  "no new history to compact",
		keep:     1,
	}
}

func TestPlanBranchFromFirstKeptUsesSelectedBoundary(t *testing.T) {
	t.Parallel()

	branch := []database.EntryEntity{
		messageEntry("old", database.RoleUser, "old context"),
		messageEntry("selected", database.RoleAssistant, "selected tail"),
		messageEntry("tail", database.RoleAssistant, "tail context"),
	}

	plan, err := PlanBranchFromFirstKept(branch, "selected", nil)

	require.NoError(t, err)
	assert.Equal(t, "selected", plan.FirstKeptEntryID)
	assert.Equal(t, []string{"old"}, plan.SummarizedEntryIDs)
	assert.Equal(t, []string{"selected", "tail"}, plan.KeptEntryIDs)
	assert.Equal(t, "old context", plan.Messages[0].Content)
	assert.Positive(t, plan.TokensBefore)
}

func TestPlanBranchCountsRunesByDefault(t *testing.T) {
	t.Parallel()

	branch := []database.EntryEntity{
		messageEntry("old", database.RoleUser, "éééé"),
		messageEntry("tail", database.RoleAssistant, "語"),
	}

	plan, err := PlanBranch(branch, 1, nil)

	require.NoError(t, err)
	assert.Equal(t, 5, plan.TokensBefore)
}

func TestCompactionSystemPromptIncludesPreviousSummary(t *testing.T) {
	t.Parallel()

	prompt := SystemPrompt("previous compacted facts", "")

	assert.Contains(t, prompt, "Update the existing compaction summary")
	assert.Contains(t, prompt, "previous compacted facts")
}

func TestCompactionSystemPromptIncludesSplitTurnSummary(t *testing.T) {
	t.Parallel()

	prompt := SystemPrompt("", "split facts")

	assert.Contains(t, prompt, "<split_turn_summary>")
	assert.Contains(t, prompt, "split facts")
}

func messageEntry(entryID string, role database.Role, content string) database.EntryEntity {
	return testEntry(entryID, database.EntryTypeMessage, role, content)
}

func testEntry(
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

func estimateTokens(text string) int {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0
	}

	return max(1, (len([]rune(trimmed))+3)/4)
}
