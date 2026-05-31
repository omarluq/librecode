//nolint:testpackage // These tests exercise unexported prompt response helpers.
package terminal

import (
	"context"
	"strings"
	"testing"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

func TestApplyStreamedSideEffectBlocks(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	blocks := []chatMessage{
		newChatMessage(database.RoleThinking, "thinking"),
		newChatMessage(database.RoleThinking, "\n\t"),
		newChatMessage(database.RoleToolResult, "tool result"),
		newChatMessage(database.RoleBashExecution, "bash result"),
		newChatMessage(database.RoleAssistant, "ignored assistant"),
		newChatMessage(database.RoleUser, "ignored user"),
		newChatMessage(database.RoleCustom, "ignored custom"),
		newChatMessage(database.RoleBranchSummary, "ignored branch"),
		newChatMessage(database.RoleCompactionSummary, "ignored compaction"),
	}

	thinkingBlocks, toolBlocks := app.applyStreamedSideEffectBlocks(blocks)

	if got, want := thinkingBlocks, 1; got != want {
		t.Fatalf("thinkingBlocks = %d, want %d", got, want)
	}
	if got, want := toolBlocks, 2; got != want {
		t.Fatalf("toolBlocks = %d, want %d", got, want)
	}
	assertPromptResponseRoles(t, app, []database.Role{
		database.RoleThinking,
		database.RoleToolResult,
		database.RoleBashExecution,
	})
}

func TestApplyPromptResponseNilClearsStreamedToolEvents(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.activePrompt = newTestActivePrompt(nil)
	app.streamedToolEvents = 2
	app.working = true

	app.applyPromptResponse(context.Background(), nil, app.activePrompt.ID)

	if got := app.streamedToolEvents; got != 0 {
		t.Fatalf("streamedToolEvents = %d, want 0", got)
	}
	if app.activePrompt != nil {
		t.Fatal("activePrompt should be cleared")
	}
}

func TestApplyRemainingSideEffectsSkipsStreamedBlocks(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	response := &assistant.PromptResponse{
		SessionID:        "",
		UserEntryID:      "",
		AssistantEntryID: "",
		Text:             "",
		Thinking:         []string{"streamed thinking", "remaining thinking"},
		ToolEvents: []assistant.ToolEvent{
			{Name: "read", ArgumentsJSON: "{}", DetailsJSON: "", Result: "streamed", Error: ""},
			{Name: "write", ArgumentsJSON: "{}", DetailsJSON: "", Result: "remaining", Error: ""},
		},
		Usage:  model.EmptyTokenUsage(),
		Cached: false,
	}

	app.applyRemainingSideEffects(response, 1, 1)

	assertPromptResponseRoles(t, app, []database.Role{
		database.RoleThinking,
		database.RoleToolResult,
	})
	if got, want := app.messages[0].Content, "remaining thinking"; got != want {
		t.Fatalf("thinking content = %q, want %q", got, want)
	}
	if got := app.messages[1].Content; !strings.Contains(got, "tool: write") {
		t.Fatalf("tool content = %q, want write tool", got)
	}
}

func assertPromptResponseRoles(t *testing.T, app *App, want []database.Role) {
	t.Helper()

	if got := len(app.messages); got != len(want) {
		t.Fatalf("message count = %d, want %d", got, len(want))
	}
	for index, role := range want {
		if got := app.messages[index].Role; got != role {
			t.Fatalf("message[%d].Role = %q, want %q", index, got, role)
		}
	}
}
