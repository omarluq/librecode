package terminal

import (
	"context"
	"time"

	"github.com/gdamore/tcell/v3"
)

func (app *App) handleForceExit() bool {
	if time.Since(app.lastControlC) <= doubleControlCDelay {
		return true
	}

	app.lastControlC = time.Now()
	app.setStatus("press Ctrl+C again to exit")

	return false
}

func (app *App) handleEscape(ctx context.Context) {
	app.handleEscapePresses(ctx, 1)
}

func (app *App) handleEscapePresses(ctx context.Context, presses int) {
	if app.working || app.compacting {
		app.handleWorkingEscape(ctx, presses)

		return
	}

	app.escapePresses = 0
	if !app.composerBuffer.Empty() {
		app.composerBuffer.Clear()
		app.resetPromptHistoryNavigation()
		app.setStatus("editor cleared")

		return
	}

	if presses >= interruptEscapePresses || time.Since(app.lastEscape) <= doubleEscapeDelay {
		app.lastEscape = time.Time{}
		if len(app.agentTaskSessionStack) > 0 {
			if err := app.leaveAgentTaskSession(ctx); err != nil {
				app.setStatus(err.Error())
			}

			return
		}

		app.openTreePanel(ctx)

		return
	}

	app.lastEscape = time.Now()
	if len(app.agentTaskSessionStack) > 0 {
		app.setStatus("escape again to return to parent session")

		return
	}

	app.setStatus("escape again to open /tree")
}

func (app *App) handleWorkingInterruptKey(ctx context.Context, event *tcell.EventKey) bool {
	if (!app.working && !app.compacting) || !isEscapeKey(event) {
		return false
	}

	app.handleWorkingEscape(ctx, escapePressCount(event))

	return true
}

func isEscapeKey(event *tcell.EventKey) bool {
	return event.Key() == tcell.KeyEscape || event.Key() == tcell.KeyEsc || event.Key() == tcell.KeyESC
}

func escapePressCount(event *tcell.EventKey) int {
	if event.Modifiers()&tcell.ModAlt != 0 {
		return interruptEscapePresses
	}

	return 1
}

func (app *App) handleWorkingEscape(ctx context.Context, presses int) {
	now := time.Now()

	if time.Since(app.lastEscape) > doubleEscapeDelay {
		app.escapePresses = 0
	}

	app.lastEscape = now

	app.escapePresses += presses
	if app.escapePresses >= interruptEscapePresses {
		app.escapePresses = 0
		app.lastEscape = time.Time{}
		app.cancelActiveOperation(ctx)

		return
	}

	app.setStatus("escape again to interrupt")
}
