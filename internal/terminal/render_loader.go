package terminal

import (
	"github.com/gdamore/tcell/v3"
	"github.com/omarluq/librecode/internal/terminal/rendertext"
)

func (app *App) renderWorkingIndicator(_ int) []rendertext.Line {
	return []rendertext.Line{
		rendertext.NewLine(tcell.StyleDefault, ""),
		rendertext.NewLine(app.workingIndicatorStyle(), app.workingIndicator()),
		rendertext.NewLine(tcell.StyleDefault, ""),
	}
}
