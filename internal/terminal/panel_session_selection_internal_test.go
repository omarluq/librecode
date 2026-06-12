package terminal

import (
	"context"
	"testing"
	"time"

	"github.com/omarluq/librecode/internal/database"
)

func TestApplySessionSelectionAddsMessageAfterSuccessfulLoad(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := newPromptSendTestApp(t, newTerminalPromptClient(newTerminalCompletionResult("ok"), nil))
	session, err := app.runtime.SessionRepository().CreateSession(ctx, app.cwd, "test", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	_, err = app.runtime.SessionRepository().AppendMessage(ctx, session.ID, nil, &database.MessageEntity{
		Timestamp: time.Time{},
		Role:      database.RoleAssistant,
		Content:   interruptTestPrompt,
		Provider:  "",
		Model:     "",
	})
	if err != nil {
		t.Fatalf("append message: %v", err)
	}

	if err := app.applySessionSelection(ctx, session.ID); err != nil {
		t.Fatalf("applySessionSelection error = %v", err)
	}

	if got, want := len(app.transcript.History), 2; got != want {
		t.Fatalf("len(messages) = %d, want %d", got, want)
	}
	if got, want := app.transcript.History[0].Content, interruptTestPrompt; got != want {
		t.Fatalf("messages[0].Content = %q, want %q", got, want)
	}
	if got, want := app.transcript.History[1].Content, "resumed session: "+session.ID; got != want {
		t.Fatalf("messages[1].Content = %q, want %q", got, want)
	}
}
