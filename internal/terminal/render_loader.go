package terminal

import (
	"github.com/gdamore/tcell/v3"
	"github.com/omarluq/librecode/internal/tui"
)

func (app *App) renderWorkingIndicator(_ int) []tui.Line {
	return []tui.Line{
		tui.NewLine(tcell.StyleDefault, ""),
		tui.NewLine(app.workingIndicatorStyle(), app.workingIndicator()),
		tui.NewLine(tcell.StyleDefault, ""),
	}
}
