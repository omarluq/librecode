package terminal

import (
	"context"
	"testing"

	"github.com/omarluq/librecode/internal/database"
)

func TestFilteredSessionEntitiesSortsAndFilters(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	sessions := []database.SessionEntity{
		testSessionEntity("b", "beta"),
		testSessionEntity("a", ""),
		testSessionEntity("c", "alpha"),
	}

	app.sessionSortRecent = false

	filtered := app.filteredSessionEntities(sessions)
	if got, want := len(filtered), 3; got != want {
		t.Fatalf("len(filtered) = %d, want %d", got, want)
	}

	if got, want := filtered[0].ID, "a"; got != want {
		t.Fatalf("filtered[0].ID = %q, want %q", got, want)
	}

	if got, want := filtered[1].Name, "alpha"; got != want {
		t.Fatalf("filtered[1].Name = %q, want %q", got, want)
	}

	app.sessionNamedOnly = true

	filtered = app.filteredSessionEntities(sessions)
	if got, want := len(filtered), 2; got != want {
		t.Fatalf("len(filtered) = %d, want %d", got, want)
	}

	if filtered[0].Name == "" || filtered[1].Name == "" {
		t.Fatal("expected only named sessions")
	}
}

func TestSessionPanelSubtitle(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	if got, want := app.sessionPanelSubtitle(), "recent • all • path off"; got != want {
		t.Fatalf("sessionPanelSubtitle() = %q, want %q", got, want)
	}

	app.sessionSortRecent = false
	app.sessionNamedOnly = true

	app.sessionShowPath = true
	if got, want := app.sessionPanelSubtitle(), "fuzzy • named • path on"; got != want {
		t.Fatalf("sessionPanelSubtitle() = %q, want %q", got, want)
	}
}

func TestSessionPanelOpenAndItems(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := newPromptSendTestApp(t, newTerminalPromptClient(newTerminalCompletionResult("ok"), nil))

	firstSession, err := app.runtime.SessionRepository().CreateSession(ctx, app.cwd, "beta", "")
	if err != nil {
		t.Fatalf("create first session: %v", err)
	}

	secondSession, err := app.runtime.SessionRepository().CreateSession(ctx, app.cwd, "alpha", "")
	if err != nil {
		t.Fatalf("create second session: %v", err)
	}

	app.openSessionPanel(ctx)

	if got, want := app.selectedPanelKind, panelSessions; got != want {
		t.Fatalf("selectedPanelKind = %q, want %q", got, want)
	}

	if app.panel == nil || app.panel.Kind() != panelSessions {
		t.Fatal("session panel should be open")
	}

	items := app.panel.Items()
	if len(items) != 2 {
		t.Fatalf("len(panel.items) = %d, want 2", len(items))
	}

	if items[0].Value != firstSession.ID && items[0].Value != secondSession.ID {
		t.Fatalf("unexpected first session item value %q", items[0].Value)
	}
}
