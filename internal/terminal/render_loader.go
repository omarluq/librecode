package terminal

import "github.com/gdamore/tcell/v3"

func (app *App) renderWorkingIndicator(_ int) []styledLine {
	return []styledLine{
		{Style: tcell.StyleDefault, Text: ""},
		{Style: app.workingIndicatorStyle(), Text: "  " + app.workingIndicator()},
		{Style: tcell.StyleDefault, Text: ""},
	}
}
