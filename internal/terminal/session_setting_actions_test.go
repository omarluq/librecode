//nolint:testpackage // These tests exercise unexported terminal toggle helpers.
package terminal

import (
	"testing"

	"github.com/omarluq/librecode/internal/model"
)

func TestToggleFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		run           func(*App)
		flag          func(*App) bool
		wantOnStatus  string
		wantOffStatus string
	}{
		{
			name:          "tools expanded",
			run:           (*App).toggleToolsExpanded,
			flag:          func(app *App) bool { return app.toolsExpanded },
			wantOnStatus:  "tool output expanded: on",
			wantOffStatus: "tool output expanded: off",
		},
		{
			name:          "thinking hidden",
			run:           (*App).toggleThinkingHidden,
			flag:          func(app *App) bool { return app.hideThinking },
			wantOnStatus:  "thinking hidden: on",
			wantOffStatus: "thinking hidden: off",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			app := newRenderTestApp(t)

			testCase.run(app)
			if !testCase.flag(app) {
				t.Fatal("flag should be true after first toggle")
			}
			if got := app.statusMessage; got != testCase.wantOnStatus {
				t.Fatalf("statusMessage = %q, want %q", got, testCase.wantOnStatus)
			}

			testCase.run(app)
			if testCase.flag(app) {
				t.Fatal("flag should be false after second toggle")
			}
			if got := app.statusMessage; got != testCase.wantOffStatus {
				t.Fatalf("statusMessage = %q, want %q", got, testCase.wantOffStatus)
			}
		})
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
