package lifecyclepayload_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant/lifecyclepayload"
	"github.com/omarluq/librecode/internal/compaction"
	"github.com/omarluq/librecode/internal/contextwindow"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/model"
)

const (
	lifecycleWorkdir       = "/work"
	lifecycleHello         = "hello"
	lifecycleAnswer        = "answer"
	lifecycleKeptEntryID   = "kept-1"
	lifecycleOldEntryID    = "old-1"
	lifecycleEntryID       = "entry-1"
	lifecycleWriteAction   = "write"
	lifecycleMainGoPath    = "main.go"
	lifecycleHistoryLabel  = "history"
	lifecycleProviderModel = "gpt-test"
)

func TestTurnStartAndContextBuildPayloads(t *testing.T) {
	t.Parallel()

	parentID := "parent-entry"
	turnStart := lifecyclepayload.TurnStart(
		lifecycleTestSessionID,
		lifecycleWorkdir,
		lifecycleHello,
		"user-1",
		&parentID,
	)
	assert.Equal(t, lifecycleTestSessionID, turnStart[lifecyclepayload.SessionIDKey])
	assert.Equal(t, parentID, turnStart[lifecyclepayload.ParentEntryIDKey])
	assert.Equal(t, lifecycleHello, turnStart[lifecyclepayload.PromptKey])

	messages := []database.MessageEntity{
		messageEntity(database.RoleUser, lifecycleHello),
		messageEntity(database.RoleAssistant, lifecycleAnswer),
		messageEntity(database.RoleAssistant, "again"),
	}
	result := &contextwindow.BuildResult{
		Breakdown:     map[string]int{contextwindow.BreakdownHistory: 10},
		SystemPrompt:  "",
		Contributions: nil,
		Messages:      nil,
		UsageAnchor:   nil,
		Usage: model.TokenUsage{
			Breakdown: map[string]int{contextwindow.BreakdownHistory: 10},
			TopContributors: []model.TokenContributor{{
				Label:   lifecycleHistoryLabel,
				Role:    string(database.RoleUser),
				Preview: lifecycleHello,
				Tokens:  5,
				Chars:   5,
			}},
			ContextWindow: 100,
			ContextTokens: 10,
			InputTokens:   9,
			OutputTokens:  1,
		},
	}
	payload := lifecyclepayload.ContextBuild(
		lifecycleTestSessionID,
		lifecycleWorkdir,
		&contextwindow.Base{
			UsageAnchor:      nil,
			BaseSystemPrompt: "",
			SkillPrompt:      "",
			SystemPrompt:     "",
			ActiveSkills:     nil,
			SkillDiagnostics: nil,
			Messages:         messages,
			HistoryTokens:    3,
			SystemTokens:     1,
			SkillTokens:      2,
		},
		result,
	)

	assert.Equal(t, 3, payload["message_count"])
	assert.Equal(t, map[string]int{"user": 1, "assistant": 2}, payload["model_facing_roles"])
	contributors, ok := payload["topContributors"].([]any)
	require.True(t, ok)
	require.Len(t, contributors, 1)
	contributor, ok := contributors[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, lifecycleHistoryLabel, contributor["label"])
}

func TestProviderResponseErrorAndNilPayloads(t *testing.T) {
	t.Parallel()

	usage := model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   100,
		ContextTokens:   20,
		InputTokens:     15,
		OutputTokens:    5,
	}
	response := lifecyclepayload.ProviderResponsePayload(&lifecyclepayload.ProviderResponse{
		FinishReason:   llm.FinishReasonLength,
		API:            "openai-responses",
		ModelID:        lifecycleProviderModel,
		Provider:       "openai",
		SessionID:      lifecycleTestSessionID,
		Text:           lifecycleAnswer,
		Usage:          usage,
		Attempt:        2,
		ThinkingCount:  1,
		ToolEventCount: 3,
	})
	assert.Equal(t, lifecycleAnswer, response[lifecyclepayload.TextKey])
	assert.Equal(t, string(llm.FinishReasonLength), response["finish_reason"])
	assert.Equal(t, 3, response["tool_event_count"])

	providerErr := lifecyclepayload.ProviderErrorPayload(&lifecyclepayload.ProviderError{
		Err:       errors.New("provider failed"),
		API:       "anthropic-messages",
		ModelID:   "claude",
		Provider:  "anthropic",
		SessionID: lifecycleTestSessionID,
		Attempt:   4,
	})
	assert.Equal(t, "provider failed", providerErr[lifecyclepayload.ErrorKey])
	assert.Equal(t, 4, providerErr[lifecyclepayload.AttemptKey])

	withoutErr := lifecyclepayload.ProviderErrorPayload(&lifecyclepayload.ProviderError{
		Err:       nil,
		API:       "",
		ModelID:   "",
		Provider:  "",
		SessionID: "",
		Attempt:   0,
	})
	assert.Equal(t, "", withoutErr[lifecyclepayload.ErrorKey])
	assert.Empty(t, lifecyclepayload.ProviderResponsePayload(nil))
	assert.Empty(t, lifecyclepayload.ProviderErrorPayload(nil))
	assert.Empty(t, lifecyclepayload.ToolResultPayload(nil))
}

func TestCompactionSavedAndDiagnosticsPayloads(t *testing.T) {
	t.Parallel()

	plan := lifecycleCompactionPlan()
	saved := lifecyclepayload.CompactionSavedPayload(lifecyclepayload.CompactionSaved{
		Entry:     lifecycleEntry(lifecycleEntryID, "summary"),
		Plan:      plan,
		SessionID: lifecycleTestSessionID,
		CWD:       lifecycleWorkdir,
		Source:    "manual",
	})
	assert.Equal(t, lifecycleEntryID, saved[lifecyclepayload.EntryIDKey])
	assert.Equal(t, "summary", saved[lifecyclepayload.SummaryKey])
	assert.Equal(t, "manual", saved["source"])

	savedWithoutEntry := lifecyclepayload.CompactionSavedPayload(lifecyclepayload.CompactionSaved{
		Entry:     nil,
		Plan:      plan,
		SessionID: lifecycleTestSessionID,
		CWD:       lifecycleWorkdir,
		Source:    "auto",
	})
	assert.Equal(t, "", savedWithoutEntry[lifecyclepayload.EntryIDKey])
	assert.Equal(t, "", savedWithoutEntry[lifecyclepayload.SummaryKey])

	diagnostic := lifecyclepayload.CompactionDiagnostics(plan, "after")
	assert.Equal(t, "after", diagnostic[lifecyclepayload.PhaseKey])
	assert.Equal(t, 1, diagnostic["summarized_entries"])
	assert.Equal(t, false, diagnostic["has_split_turn_summary"])

	nilDiagnostic := lifecyclepayload.CompactionDiagnostics(nil, "before")
	assert.Equal(t, map[string]any{lifecyclepayload.PhaseKey: "before"}, nilDiagnostic)
}

func TestTokenContributorAndUtilityPayloads(t *testing.T) {
	t.Parallel()

	contributors := lifecyclepayload.TokenContributors([]model.TokenContributor{{
		Label:   contextwindow.BreakdownSystem,
		Role:    contextwindow.ContributionRoleSystem,
		Preview: "prompt",
		Tokens:  10,
		Chars:   30,
	}})
	require.Len(t, contributors, 1)
	contributor, ok := contributors[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 10, contributor["tokens"])

	assert.Equal(t, []any{"a", "b"}, lifecyclepayload.StringSlice([]string{"a", "b"}))
	assert.Equal(t, 1.5, lifecyclepayload.DurationMilliseconds(1500*time.Microsecond))
}

func messageEntity(role database.Role, content string) database.MessageEntity {
	return database.MessageEntity{
		Timestamp: time.Time{},
		Role:      role,
		Content:   content,
		Provider:  "",
		Model:     "",
	}
}

func lifecycleCompactionPlan() *compaction.Plan {
	return &compaction.Plan{
		FirstKeptEntryID: lifecycleKeptEntryID,
		Messages:         nil,
		PreviousSummary:  "",
		SplitTurnSummary: "",
		SummarizedEntryIDs: []string{
			lifecycleOldEntryID,
		},
		KeptEntryIDs: []string{
			lifecycleKeptEntryID,
		},
		FileOperations: []compaction.FileOperation{{
			EntryID: lifecycleEntryID,
			Action:  lifecycleWriteAction,
			Path:    lifecycleMainGoPath,
			Tool:    lifecycleWriteAction,
			Command: "",
		}},
		TokensBefore:        42,
		FirstKeptEntryIndex: 0,
	}
}

func lifecycleEntry(entryID, summary string) *database.EntryEntity {
	return &database.EntryEntity{
		CreatedAt:                  time.Time{},
		ParentID:                   nil,
		Message:                    messageEntity("", ""),
		Summary:                    summary,
		ToolStatus:                 "",
		Type:                       database.EntryTypeMessage,
		CustomType:                 "",
		DataJSON:                   "",
		ID:                         entryID,
		ToolName:                   "",
		SessionID:                  lifecycleTestSessionID,
		ToolArgsJSON:               "",
		BranchFromEntryID:          "",
		CompactionFirstKeptEntryID: "",
		CompactionTokensBefore:     0,
		TokenEstimate:              0,
		Display:                    true,
		ModelFacing:                true,
	}
}
