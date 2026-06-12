package lifecyclepayload_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/assistant/lifecyclepayload"
	"github.com/omarluq/librecode/internal/compaction"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

const lifecycleTestSessionID = "session-1"

func TestPromptAndTurnPayloads(t *testing.T) {
	t.Parallel()

	parentID := "parent-1"
	prompt := lifecyclepayload.Prompt(&lifecyclepayload.PromptRequest{
		ParentEntryID: &parentID,
		CWD:           "/work",
		Name:          "agent",
		SessionID:     lifecycleTestSessionID,
		Text:          "hello",
		ResumeLatest:  true,
	})
	assert.Equal(t, "/work", prompt[lifecyclepayload.CWDKey])
	assert.Equal(t, "agent", prompt[lifecyclepayload.ToolNameKey])
	assert.Equal(t, parentID, prompt[lifecyclepayload.ParentEntryIDKey])
	assert.Equal(t, true, prompt["resume_latest"])
	assert.Empty(t, lifecyclepayload.Prompt(nil))

	turn := lifecyclepayload.TurnEndPayload(&lifecyclepayload.TurnEnd{
		Err: errors.New("failed"),
		Usage: model.TokenUsage{
			Breakdown:       map[string]int{"history": 2},
			TopContributors: nil,
			ContextWindow:   0,
			ContextTokens:   0,
			InputTokens:     3,
			OutputTokens:    0,
		},
		AssistantEntryID: "assistant-1",
		SessionID:        lifecycleTestSessionID,
		UserEntryID:      "user-1",
		Cached:           true,
	})
	assert.Equal(t, "assistant-1", turn[lifecyclepayload.AssistantEntryIDKey])
	assert.Equal(t, "failed", turn[lifecyclepayload.ErrorKey])
	assert.Equal(t, true, turn["cached"])
	usage, ok := turn[lifecyclepayload.UsageKey].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, 3, usage[lifecyclepayload.InputTokensKey])
}

func TestSessionEntryAndContextPayloads(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 6, 9, 1, 2, 3, 4, time.UTC)
	session := lifecyclepayload.Session(&database.SessionEntity{
		ID:            lifecycleTestSessionID,
		ParentSession: "parent-session",
		Name:          "main",
		CWD:           "/work",
		CreatedAt:     createdAt,
		UpdatedAt:     createdAt,
	})
	assert.Equal(t, createdAt.Format(lifecyclepayload.TimeFormatRFC3339Nano), session[lifecyclepayload.CreatedAtKey])
	assert.Equal(t, "parent-session", session[lifecyclepayload.ParentSessionKey])
	assert.Empty(t, lifecyclepayload.Session(nil))

	parentID := "parent-entry"
	entry := lifecyclepayload.Entry(&database.EntryEntity{
		CreatedAt: createdAt,
		ParentID:  &parentID,
		Message: database.MessageEntity{
			Timestamp: createdAt,
			Role:      database.RoleAssistant,
			Content:   "answer",
			Provider:  "provider-1",
			Model:     "model-1",
		},
		Summary:                    "summary",
		ToolStatus:                 "",
		Type:                       database.EntryTypeMessage,
		CustomType:                 "",
		DataJSON:                   "",
		ID:                         "entry-1",
		ToolName:                   "",
		SessionID:                  lifecycleTestSessionID,
		ToolArgsJSON:               "",
		BranchFromEntryID:          "",
		CompactionFirstKeptEntryID: "",
		CompactionTokensBefore:     0,
		TokenEstimate:              5,
		Display:                    true,
		ModelFacing:                true,
	})
	assert.Equal(t, "entry-1", entry[lifecyclepayload.EntryIDKey])
	assert.Equal(t, "assistant", entry[lifecyclepayload.RoleKey])
	assert.Equal(t, "answer", entry[lifecyclepayload.TextKey])
}

func TestProviderAndToolPayloads(t *testing.T) {
	t.Parallel()

	providerRequest := lifecyclepayload.ProviderRequestPayload(&lifecyclepayload.ProviderRequest{
		Payload:       map[string]any{"messages": 1},
		Headers:       map[string]string{"X-Test": "yes"},
		API:           "openai-responses",
		ModelID:       "model-1",
		Provider:      "openai",
		SessionID:     lifecycleTestSessionID,
		ThinkingLevel: "off",
		Attempt:       2,
	})
	assert.Equal(t, "openai-responses", providerRequest[lifecyclepayload.APIKey])
	assert.Equal(t, 2, providerRequest[lifecyclepayload.AttemptKey])
	assert.Equal(t, map[string]string{"X-Test": "yes"}, providerRequest[lifecyclepayload.ProviderHeadersKey])

	toolCall := lifecyclepayload.ToolCallPayload(lifecyclepayload.ToolCall{
		Arguments:     map[string]any{"path": "README.md"},
		ID:            "call-1",
		Name:          "read",
		ArgumentsJSON: `{"path":"README.md"}`,
	})
	assert.Equal(t, "call-1", toolCall["call_id"])
	assert.Equal(t, "read", toolCall[lifecyclepayload.ToolNameKey])

	toolResult := lifecyclepayload.ToolResultPayload(&lifecyclepayload.ToolResult{
		Name:          "read",
		ArgumentsJSON: "{}",
		DetailsJSON:   "{}",
		Result:        "ok",
		Error:         "failed",
		IsError:       true,
	})
	assert.Equal(t, true, toolResult["is_error"])
	assert.Equal(t, "failed", toolResult[lifecyclepayload.ToolErrorKey])
}

func TestCompactionAndDiagnosticPayloads(t *testing.T) {
	t.Parallel()

	plan := &compaction.Plan{
		FirstKeptEntryID: "kept-1",
		Messages:         nil,
		PreviousSummary:  "",
		SplitTurnSummary: "split",
		SummarizedEntryIDs: []string{
			"old-1",
		},
		KeptEntryIDs: []string{
			"kept-1",
		},
		FileOperations: []compaction.FileOperation{
			{
				EntryID: "entry-1",
				Action:  "write",
				Path:    "main.go",
				Tool:    "write",
				Command: "",
			},
		},
		TokensBefore:        42,
		FirstKeptEntryIndex: 1,
	}
	payload := lifecyclepayload.CompactionPreparation(lifecycleTestSessionID, "/work", plan)
	assert.Equal(t, "kept-1", payload["first_kept_entry_id"])
	assert.Equal(t, 42, payload[lifecyclepayload.TokensBeforeKey])
	assert.Equal(t, []any{"old-1"}, payload["summarized_entry_ids"])

	nilPlanPayload := lifecyclepayload.CompactionPreparation(lifecycleTestSessionID, "/work", nil)
	assert.Equal(t, lifecycleTestSessionID, nilPlanPayload[lifecyclepayload.SessionIDKey])
	assert.Equal(t, "/work", nilPlanPayload[lifecyclepayload.CWDKey])
	assert.Empty(t, nilPlanPayload["first_kept_entry_id"])
	assert.Equal(t, []any{}, nilPlanPayload["summarized_entry_ids"])
	assert.Equal(t, []any{}, nilPlanPayload[compaction.FileOperationsKey])

	diagnostic := lifecyclepayload.Diagnostic(
		"event",
		2,
		1500*time.Microsecond,
		[]string{"oops"},
		map[string]any{"extra": true},
	)
	assert.Equal(t, 2, diagnostic[lifecyclepayload.HookCountKey])
	assert.InDelta(t, 1.5, diagnostic[lifecyclepayload.DurationMsKey], 0)
	assert.Equal(t, []string{"oops"}, diagnostic[lifecyclepayload.ErrorsKey])
	assert.Equal(t, true, diagnostic["extra"])
}
