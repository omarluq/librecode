//nolint:testpackage // These tests validate unexported session panel helpers directly.
package terminal

import (
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
