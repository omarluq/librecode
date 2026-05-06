package terminal

import "github.com/gdamore/tcell/v3"

func (app *App) handleTranscriptScroll(event *tcell.EventKey) bool {
	if app.keys.matches(event, actionSelectPageUp) {
		app.scrollTranscript(app.transcriptScrollRows())
		return true
	}
	if app.keys.matches(event, actionSelectPageDown) {
		app.scrollTranscript(-app.transcriptScrollRows())
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
		app.scrollTranscript(app.transcriptScrollRows())
		return
	}
	if buttons&tcell.WheelDown != 0 {
		app.scrollTranscript(-app.transcriptScrollRows())
	}
}

func (app *App) transcriptScrollRows() int {
	_, height := app.screen.Size()

	return max(3, (height-footerReserve())/2)
}

func (app *App) scrollTranscript(delta int) {
	app.scrollOffset = max(0, app.scrollOffset+delta)
	if app.scrollOffset == 0 {
		app.setStatus("scroll: bottom")
		return
	}
	app.setStatus("scroll: " + intText(app.scrollOffset) + " lines up")
}

func (app *App) visibleMessageLines(lines []styledLine, maxRows int) []styledLine {
	if maxRows < 0 || len(lines) <= maxRows {
		app.scrollOffset = 0
		return lines
	}
	maxOffset := max(0, len(lines)-maxRows)
	app.scrollOffset = min(app.scrollOffset, maxOffset)
	end := len(lines) - app.scrollOffset
	start := max(0, end-maxRows)

	return lines[start:end]
}
