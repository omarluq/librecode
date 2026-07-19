package terminal

import (
	"testing"

	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/terminal/extui"
)

type cursorRecordingScreen struct {
	terminalScreen
	calls []string
}

func (screen *cursorRecordingScreen) HideCursor() {
	screen.calls = append(screen.calls, "hide")
}

func (screen *cursorRecordingScreen) ShowCursor(_, _ int) {
	screen.calls = append(screen.calls, "show")
}

func TestFocusedRuntimeListsHideExtensionCursor(t *testing.T) {
	t.Parallel()

	t.Run("transcript list", func(t *testing.T) {
		t.Parallel()
		assertFocusedRuntimeListHidesExtensionCursor(t, func(app *App) {
			app.transcriptList.Active = true
		})
	})
	t.Run("agent task summary", func(t *testing.T) {
		t.Parallel()
		assertFocusedRuntimeListHidesExtensionCursor(t, func(app *App) {
			app.agentTaskSummarySelection.Active = true
		})
	})
}

func assertFocusedRuntimeListHidesExtensionCursor(t *testing.T, focus func(*App)) {
	t.Helper()

	screen := &cursorRecordingScreen{terminalScreen: newClipboardScreen(), calls: nil}
	app := newRenderTestApp(t)
	app.screen = screen
	app.extensionUI.Cursor = &extension.UICursor{Window: extui.BufferComposer, Row: 1, Col: 2}
	focus(app)

	layout := app.defaultRuntimeLayout(40, 12)
	app.showRuntimeCursor(&layout)

	if len(screen.calls) != 1 || screen.calls[0] != "hide" {
		t.Fatalf("cursor calls = %v, want [hide]", screen.calls)
	}
}
