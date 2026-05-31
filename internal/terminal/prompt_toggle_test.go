//nolint:testpackage // These tests exercise unexported terminal toggle helpers.
package terminal

import (
	"testing"

	"github.com/omarluq/librecode/internal/model"
)

func TestToggleToolsExpanded(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)

	app.toggleToolsExpanded()

	if !app.toolsExpanded {
		t.Fatal("toolsExpanded should be true after first toggle")
	}
	if got, want := app.statusMessage, "tool output expanded: on"; got != want {
		t.Fatalf("statusMessage = %q, want %q", got, want)
	}

	app.toggleToolsExpanded()

	if app.toolsExpanded {
		t.Fatal("toolsExpanded should be false after second toggle")
	}
	if got, want := app.statusMessage, "tool output expanded: off"; got != want {
		t.Fatalf("statusMessage = %q, want %q", got, want)
	}
}

func TestToggleThinkingHidden(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)

	app.toggleThinkingHidden()

	if !app.hideThinking {
		t.Fatal("hideThinking should be true after first toggle")
	}
	if got, want := app.statusMessage, "thinking hidden: on"; got != want {
		t.Fatalf("statusMessage = %q, want %q", got, want)
	}

	app.toggleThinkingHidden()

	if app.hideThinking {
		t.Fatal("hideThinking should be false after second toggle")
	}
	if got, want := app.statusMessage, "thinking hidden: off"; got != want {
		t.Fatalf("statusMessage = %q, want %q", got, want)
	}
}

func TestCycleThinking(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.cfg = renderParityConfig()
	app.cfg.Assistant.ThinkingLevel = ""

	app.cycleThinking()
	if got, want := app.currentThinkingLevel(), string(model.ThinkingMinimal); got != want {
		t.Fatalf("thinking level = %q, want %q", got, want)
	}

	app.setThinkingLevel(string(model.ThinkingXHigh))
	app.cycleThinking()
	if got, want := app.currentThinkingLevel(), string(model.ThinkingOff); got != want {
		t.Fatalf("thinking level = %q, want %q", got, want)
	}
}
