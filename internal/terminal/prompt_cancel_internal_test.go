package terminal

import (
	"context"
	"strings"
	"testing"

	"github.com/omarluq/librecode/internal/transcript"
)

func TestCancelActivePromptPreservesQueuedMessages(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.addMessage(transcript.RoleUser, "prompt")
	app.queuedMessages = []string{"follow up"}
	app.activePrompt = newTestActivePrompt(func() {})
	app.activePrompt.BaselineMessages = 0
	app.activePrompt.Prompt = "prompt"

	app.cancelActivePrompt(context.Background())

	if got, want := len(app.transcript.History), 0; got != want {
		t.Fatalf("messages length = %d, want %d", got, want)
	}

	if got, want := app.queuedMessages, []string{"follow up"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("queuedMessages = %v, want %v", got, want)
	}

	if app.activePrompt != nil {
		t.Fatal("activePrompt should be cleared")
	}
}

func TestCancelActivePromptWithoutActivePromptClearsTransientState(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.working = true
	app.streamingText = "partial"
	app.streamingThinkingText = "thinking"
	app.transcript.Streaming.Blocks = []chatMessage{newChatMessage(transcript.RoleAssistant, "partial")}
	app.streamedToolEvents = 2

	app.cancelActivePrompt(context.Background())

	if app.working {
		t.Fatal("working should be false")
	}

	if app.streamingText != "" || app.streamingThinkingText != "" {
		t.Fatalf(
			"streaming text should be cleared, got text=%q thinking=%q",
			app.streamingText,
			app.streamingThinkingText,
		)
	}

	if len(app.transcript.Streaming.Blocks) != 0 {
		t.Fatalf("streamingBlocks length = %d, want 0", len(app.transcript.Streaming.Blocks))
	}

	if app.streamedToolEvents != 0 {
		t.Fatalf("streamedToolEvents = %d, want 0", app.streamedToolEvents)
	}

	if got, want := app.statusMessage, "no active response to cancel"; got != want {
		t.Fatalf("statusMessage = %q, want %q", got, want)
	}
}

func TestDeleteCanceledPromptBranchFailureKeepsCanceledPrompt(t *testing.T) {
	t.Parallel()

	client := newTerminalPromptClient(newTerminalCompletionResult("ok"), nil)
	app := newPromptSendTestApp(t, client)
	app.activePrompt = newTestActivePrompt(func() {})
	app.activePrompt.SessionID = "missing-session"
	app.activePrompt.UserEntryID = "missing-entry"
	promptID := app.activePrompt.ID
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	app.cancelActivePrompt(ctx)

	if _, ok := app.canceledPrompts[promptID]; !ok {
		t.Fatal("canceled prompt should remain tracked when persisted branch deletion fails")
	}

	if !strings.Contains(app.statusMessage, "failed to revert persisted branch") {
		t.Fatalf("statusMessage = %q, want failed revert message", app.statusMessage)
	}
}
