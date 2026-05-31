//nolint:testpackage // These tests exercise unexported prompt cancellation helpers.
package terminal

import (
	"context"
	"testing"

	"github.com/omarluq/librecode/internal/database"
)

func TestCancelActivePromptPreservesQueuedMessages(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.addMessage(database.RoleUser, "prompt")
	app.queuedMessages = []string{"follow up"}
	app.activePrompt = newTestActivePrompt(func() {})
	app.activePrompt.BaselineMessages = 0
	app.activePrompt.Prompt = "prompt"

	app.cancelActivePrompt(context.Background())

	if got, want := len(app.messages), 0; got != want {
		t.Fatalf("messages length = %d, want %d", got, want)
	}
	if got, want := app.queuedMessages, []string{"follow up"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("queuedMessages = %v, want %v", got, want)
	}
	if app.activePrompt != nil {
		t.Fatal("activePrompt should be cleared")
	}
}
