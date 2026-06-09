//nolint:testpackage // These tests exercise unexported prompt response helpers.
package terminal

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/transcript"
)

func TestApplyStreamedSideEffectBlocks(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	blocks := []chatMessage{
		newChatMessage(transcript.RoleThinking, "thinking"),
		newChatMessage(transcript.RoleThinking, "\n\t"),
		newChatMessage(transcript.RoleToolResult, "tool result"),
		newChatMessage(transcript.RoleBashExecution, "bash result"),
		newChatMessage(transcript.RoleAssistant, "ignored assistant"),
		newChatMessage(transcript.RoleUser, "ignored user"),
		newChatMessage(transcript.RoleCustom, "ignored custom"),
		newChatMessage(transcript.RoleBranchSummary, "ignored branch"),
		newChatMessage(transcript.RoleCompactionSummary, "ignored compaction"),
	}

	thinkingBlocks, toolBlocks := app.applyStreamedSideEffectBlocks(blocks)

	if got, want := thinkingBlocks, 1; got != want {
		t.Fatalf("thinkingBlocks = %d, want %d", got, want)
	}
	if got, want := toolBlocks, 2; got != want {
		t.Fatalf("toolBlocks = %d, want %d", got, want)
	}
	assertPromptResponseRoles(t, app, []transcript.Role{
		transcript.RoleThinking,
		transcript.RoleToolResult,
		transcript.RoleBashExecution,
	})
}

func TestApplyPromptResponseNilClearsStreamedToolEvents(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.activePrompt = newTestActivePrompt(nil)
	app.streamedToolEvents = 2
	app.working = true
	app.appendStreamingBlock(transcript.RoleAssistant, "partial")

	app.applyPromptResponse(context.Background(), nil, app.activePrompt.ID)

	if got := app.streamedToolEvents; got != 0 {
		t.Fatalf("streamedToolEvents = %d, want 0", got)
	}
	if app.activePrompt != nil {
		t.Fatal("activePrompt should be cleared")
	}
	if app.working {
		t.Fatal("working should be false")
	}
	assert.Empty(t, app.transcript.Streaming.Blocks)
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
			{Name: "read", ArgumentsJSON: "{}", DetailsJSON: "", Result: "streamed", Error: "", IsError: false},
			{Name: "write", ArgumentsJSON: "{}", DetailsJSON: "", Result: "remaining", Error: "", IsError: false},
		},
		Usage:  model.EmptyTokenUsage(),
		Cached: false,
	}

	app.applyRemainingSideEffects(response, 1, 1)

	assertPromptResponseRoles(t, app, []transcript.Role{
		transcript.RoleThinking,
		transcript.RoleToolResult,
	})
	if got, want := app.transcript.History[0].Content, "remaining thinking"; got != want {
		t.Fatalf("thinking content = %q, want %q", got, want)
	}
	if got := app.transcript.History[1].Content; !strings.Contains(got, "tool: write") {
		t.Fatalf("tool content = %q, want write tool", got)
	}
}

func assertPromptResponseRoles(t *testing.T, app *App, want []transcript.Role) {
	t.Helper()

	if got := len(app.transcript.History); got != len(want) {
		t.Fatalf("message count = %d, want %d", got, len(want))
	}
	for index, role := range want {
		if got := app.transcript.History[index].Role; got != role {
			t.Fatalf("message[%d].Role = %q, want %q", index, got, role)
		}
	}
}

func TestApplyPromptResponseIgnoresCanceledPrompt(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.addMessage(transcript.RoleUser, "kept")
	app.canceledPrompts[42] = newTestActivePrompt(nil)
	app.working = true

	app.applyPromptResponse(context.Background(), newTestPromptResponse("ignored"), 42)

	if got, want := len(app.transcript.History), 1; got != want {
		t.Fatalf("messages length = %d, want %d", got, want)
	}
	if _, ok := app.canceledPrompts[42]; ok {
		t.Fatal("canceled prompt should be consumed")
	}
}

func TestApplyPromptResponseClearsCanceledActivePrompt(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.activePrompt = newTestActivePrompt(nil)
	app.activePrompt.Canceled = true
	app.appendStreamingBlock(transcript.RoleAssistant, "ignored stream")

	app.applyPromptResponse(context.Background(), newTestPromptResponse("ignored"), app.activePrompt.ID)

	if app.activePrompt != nil {
		t.Fatal("activePrompt should be cleared")
	}
}

func TestApplyPromptResponseAddsAssistantAndProcessesQueue(t *testing.T) {
	t.Parallel()

	client := newTerminalPromptClient(newTerminalCompletionResult("queued response"), nil)
	app := newPromptSendTestApp(t, client)
	app.activePrompt = newTestActivePrompt(nil)
	app.queuedMessages = []string{"queued"}

	app.applyPromptResponse(context.Background(), newTestPromptResponse("assistant response"), app.activePrompt.ID)

	if got, want := app.transcript.History[0].Content, "assistant response"; got != want {
		t.Fatalf("assistant message = %q, want %q", got, want)
	}
	if !app.working {
		t.Fatal("queued prompt should start after response")
	}
	if got, want := app.queuedMessages, []string(nil); !slices.Equal(got, want) {
		t.Fatalf("queuedMessages = %v, want empty", got)
	}
}

func newTestPromptResponse(text string) *assistant.PromptResponse {
	return &assistant.PromptResponse{
		SessionID:        "",
		UserEntryID:      "",
		AssistantEntryID: "",
		Text:             text,
		Thinking:         nil,
		ToolEvents:       nil,
		Usage:            model.EmptyTokenUsage(),
		Cached:           false,
	}
}
