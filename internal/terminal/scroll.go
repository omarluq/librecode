package terminal

import "github.com/gdamore/tcell/v3"

const (
	keyboardScrollRows = 5
	mouseScrollRows    = 2
)

type coalescedScrollEvent struct {
	Pending tcell.Event
	Delta   int
}

func (app *App) handleTranscriptScroll(event *tcell.EventKey) bool {
	delta, ok := app.keyScrollDelta(event)
	if !ok {
		return false
	}

	app.scrollTranscript(delta)

	return true
}

func (app *App) handleMouse(event *tcell.EventMouse) {
	if app.mode != modeChat {
		return
	}

	if delta, ok := app.mouseScrollDelta(event); ok {
		app.scrollTranscript(delta)

		return
	}

	column, row := event.Position()
	if event.Buttons()&tcell.ButtonPrimary != 0 {
		if app.selection.active {
			app.updateMouseSelection(column, row)

			return
		}

		app.beginMouseSelection(column, row, event.When())

		return
	}

	if app.selection.active {
		app.finishMouseSelection(column, row)
	}
}

func (app *App) scrollDeltaForEvent(event tcell.Event) (int, bool) {
	if app.mode != modeChat {
		return 0, false
	}

	switch typedEvent := event.(type) {
	case *tcell.EventKey:
		return app.keyScrollDelta(typedEvent)
	case *tcell.EventMouse:
		return app.mouseScrollDelta(typedEvent)
	default:
		return 0, false
	}
}

func (app *App) keyScrollDelta(event *tcell.EventKey) (int, bool) {
	if app.keys.matches(event, actionSelectPageUp) {
		return keyboardScrollRows, true
	}

	if app.keys.matches(event, actionSelectPageDown) {
		return -keyboardScrollRows, true
	}

	return 0, false
}

func (app *App) mouseScrollDelta(event *tcell.EventMouse) (int, bool) {
	buttons := event.Buttons()
	if buttons&tcell.WheelUp != 0 {
		return mouseScrollRows, true
	}

	if buttons&tcell.WheelDown != 0 {
		return -mouseScrollRows, true
	}

	return 0, false
}

func (app *App) coalesceScrollEvents(delta int) coalescedScrollEvent {
	coalesced := coalescedScrollEvent{Pending: nil, Delta: delta}

	for {
		select {
		case event := <-app.screen.EventQ():
			nextDelta, ok := app.scrollDeltaForEvent(event)
			if !ok {
				coalesced.Pending = event

				return coalesced
			}

			coalesced.Delta += nextDelta
		default:
			return coalesced
		}
	}
}

func (app *App) scrollTranscript(delta int) {
	app.scrollOffset = max(0, app.scrollOffset+delta)
}
