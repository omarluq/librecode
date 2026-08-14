package compaction

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/contextwindow"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/tool"
)

func TestCollectValidationRecordsLatestOutcomeAndPriorCompaction(t *testing.T) {
	t.Parallel()

	prior := appendixEntry(t, "compact", database.EntryTypeCompaction, "session", map[string]any{
		"validation_records": []ValidationRecord{{EntryID: "old", Command: "go test ./...", Outcome: ValidationFailed}},
	})
	entries := []database.EntryEntity{
		prior,
		validationEntry("retry", "go test ./...", "completed"),
		validationEntry("cancel", "task ci", "canceled"),
		validationEntry("unknown", "task build", "mystery"),
	}

	assert.Equal(t, []ValidationRecord{
		{EntryID: "retry", Command: "go test ./...", Outcome: ValidationPassed},
		{EntryID: "cancel", Command: "task ci", Outcome: ValidationCanceled},
		{EntryID: "unknown", Command: "task build", Outcome: ValidationUnknown},
	}, CollectValidationRecords(entries))
}

func TestCollectActiveWorkRecordsLatestStateAndKinds(t *testing.T) {
	t.Parallel()

	entries := []database.EntryEntity{
		appendixEntry(t, "queued", database.EntryTypeMessage, "owner", map[string]any{
			activeDetailsKindKey:   activeKindAgent,
			activeDetailsTaskIDKey: "agent-1",
			activeDetailsStateKey:  activeStateQueued,
		}),
		appendixEntry(t, "done", database.EntryTypeMessage, "owner", map[string]any{
			activeDetailsKindKey:   activeKindAgent,
			activeDetailsTaskIDKey: "agent-1",
			activeDetailsStateKey:  "completed",
		}),
		appendixEntry(t, "tool", database.EntryTypeMessage, "owner", map[string]any{
			activeDetailsKindKey:   activeKindToolAlias,
			activeDetailsTaskIDKey: "tool-1",
			activeDetailsStateKey:  activeStateRunning,
		}),
		appendixEntry(t, "workflow", database.EntryTypeMessage, "owner", map[string]any{
			activeDetailsKindKey: activeKindWorkflow, "run_id": "run-1",
		}),
		appendixEntry(t, "canceled", database.EntryTypeMessage, "owner", map[string]any{
			activeDetailsKindKey: activeKindWorkflow, "workflow_task_id": "run-2", activeDetailsStateKey: "canceled",
		}),
	}

	assert.Equal(t, []ActiveWorkRecord{
		{EntryID: "tool", ID: "tool-1", Kind: "background_tool", State: "running", OwningSession: "owner"},
		{EntryID: "workflow", ID: "run-1", Kind: "workflow", State: "unknown", OwningSession: "owner"},
	}, CollectActiveWorkRecords(entries))
}

func TestCollectValidationRecordsExcludesNonValidationBash(t *testing.T) {
	t.Parallel()

	entries := []database.EntryEntity{
		validationEntry("edit", "sed -i 's/a/b/' file.go", "completed"),
		validationEntry("test", "mise exec -- go test ./...", "completed"),
	}

	assert.Equal(t, []ValidationRecord{{
		EntryID: "test", Command: "mise exec -- go test ./...", Outcome: ValidationPassed,
	}}, CollectValidationRecords(entries))
}

func TestAppendixIndependentTokenBoundsAndNewestRetention(t *testing.T) {
	t.Parallel()

	tests := []struct {
		heading string
		limit   int
	}{
		{heading: "### files", limit: 24},
		{heading: "### validations", limit: 30},
		{heading: "### work", limit: 20},
	}

	for _, testCase := range tests {
		t.Run(testCase.heading, func(t *testing.T) {
			t.Parallel()

			lines := make([]string, 12)
			for index := range lines {
				lines[index] = fmt.Sprintf("- record-%02d %s", index, strings.Repeat("x", 12))
			}

			sections := appendBoundedSection(nil, testCase.heading, lines, testCase.limit)
			require.Len(t, sections, 1)
			assert.LessOrEqual(t, contextwindow.EstimateTokens(sections[0]), testCase.limit)
			assert.Contains(t, sections[0], "record-11")
			assert.NotContains(t, sections[0], "record-00")
			assert.Contains(t, sections[0], "older records omitted")
		})
	}
}

func TestAppendBoundedSectionOmitsMarkerWhenItCannotFit(t *testing.T) {
	t.Parallel()

	heading := "### nearly full"
	limit := contextwindow.EstimateTokens(heading)
	sections := appendBoundedSection(nil, heading, []string{"- omitted"}, limit)

	require.Len(t, sections, 1)
	assert.LessOrEqual(t, contextwindow.EstimateTokens(sections[0]), limit)
	assert.NotContains(t, sections[0], "older records omitted")
}

func TestAppendBoundedSectionKeepsContiguousNewestSuffix(t *testing.T) {
	t.Parallel()

	heading := "### records"
	newest := "- newest"
	marker := "- ... 2 older records omitted"
	limit := contextwindow.EstimateTokens(heading) + contextwindow.EstimateTokens(newest) +
		contextwindow.EstimateTokens(marker)
	sections := appendBoundedSection(nil, heading, []string{"- tiny-old", strings.Repeat("x", 100), newest}, limit)

	require.Len(t, sections, 1)
	assert.Contains(t, sections[0], newest)
	assert.NotContains(t, sections[0], "tiny-old")
}

func TestAppendDeterministicStateReplacesStaleAppendices(t *testing.T) {
	t.Parallel()

	first := AppendDeterministicState(validCheckpoint,
		[]FileOperation{{EntryID: "", Action: "read", Path: "old.go", Tool: "", Command: ""}},
		[]ValidationRecord{{EntryID: "one", Command: "go test", Outcome: ValidationFailed}},
		[]ActiveWorkRecord{{
			EntryID: "", ID: "task-1", Kind: activeKindAgentTask,
			State: activeStateRunning, OwningSession: "",
		}},
	)
	second := AppendDeterministicState(first,
		[]FileOperation{{EntryID: "", Action: "modified", Path: "new.go", Tool: "", Command: ""}},
		[]ValidationRecord{{EntryID: "two", Command: "go test", Outcome: ValidationPassed}}, nil,
	)

	assert.NotContains(t, second, "old.go")
	assert.NotContains(t, second, "task-1")
	assert.Contains(t, second, "modified: new.go")
	assert.Contains(t, second, "passed: go test (entry two)")
	assert.Equal(t, 1, strings.Count(second, "### Librecode validation records"))
}

func validationEntry(id, command, status string) database.EntryEntity {
	return database.EntryEntity{
		CreatedAt: time.Time{}, ParentID: nil, ToolStatus: status, SessionID: "",
		ToolArgsJSON: `{"command":` + fmt.Sprintf("%q", command) + `}`, CustomType: "", DataJSON: "",
		ID: id, Summary: "", ToolName: string(tool.NameBash), Type: "", BranchFromEntryID: "",
		CompactionFirstKeptEntryID: "", Message: emptyAppendixMessage(), CompactionTokensBefore: 0,
		TokenEstimate: 0, Display: false, ModelFacing: false,
	}
}

func appendixEntry(
	t *testing.T,
	entryID string,
	entryType database.EntryType,
	session string,
	details map[string]any,
) database.EntryEntity {
	t.Helper()

	data, err := json.Marshal(map[string]any{"details": details})
	require.NoError(t, err)

	return database.EntryEntity{
		CreatedAt: time.Time{}, ParentID: nil, ToolStatus: "", SessionID: session,
		ToolArgsJSON: "", CustomType: "", DataJSON: string(data), ID: entryID, Summary: "", ToolName: "",
		Type: entryType, BranchFromEntryID: "", CompactionFirstKeptEntryID: "",
		Message: emptyAppendixMessage(), CompactionTokensBefore: 0, TokenEstimate: 0,
		Display: false, ModelFacing: false,
	}
}

func emptyAppendixMessage() database.MessageEntity {
	return database.MessageEntity{
		Timestamp: time.Time{}, Role: "", Content: "", Provider: "", Model: "", Parts: nil,
	}
}
