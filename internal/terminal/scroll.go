package terminal

import "github.com/gdamore/tcell/v3"

const (
	keyboardScrollRows = 5
	mouseScrollRows    = 2
)

func (app *App) handleTranscriptScroll(event *tcell.EventKey) bool {
	if app.keys.matches(event, actionSelectPageUp) {
		app.scrollTranscript(keyboardScrollRows)
		return true
	}
	if app.keys.matches(event, actionSelectPageDown) {
		app.scrollTranscript(-keyboardScrollRows)
		return true
	}

	return false
}

func (app *App) handleMouse(event *tcell.EventMouse) {
	if app.mode != modeChat {
		return
	}
	buttons := event.Buttons()
	if buttons&tcell.WheelUp != 0 {
		app.scrollTranscript(mouseScrollRows)
		return
	}
	if buttons&tcell.WheelDown != 0 {
		app.scrollTranscript(-mouseScrollRows)
	}
}

func (app *App) scrollTranscript(delta int) {
	app.scrollOffset = max(0, app.scrollOffset+delta)
}
