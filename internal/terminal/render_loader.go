package terminal

import "github.com/gdamore/tcell/v3"

func (app *App) renderWorkingIndicator(_ int) []styledLine {
	return []styledLine{
		newStyledLine(tcell.StyleDefault, ""),
		newStyledLine(app.workingIndicatorStyle(), app.workingIndicator()),
		newStyledLine(tcell.StyleDefault, ""),
	}
}
