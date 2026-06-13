package terminal

import "github.com/omarluq/librecode/internal/terminal/extui"

func (app *App) drawPanel(width, height, row int) int {
	availableHeight := max(1, height-row-app.composerReserve(width, height))
	options := panelRenderOptions(width, availableHeight, app.theme, app.keys)

	lines := app.panel.Render(&options)
	for _, line := range lines {
		app.writeStyledLine(row, width, line)
		row++
	}

	return row
}

func (app *App) drawPanelWindow(layout *extui.Layout) {
	window := layout.Transcript
	if !window.Visible || window.Height <= 0 || app.extensionOwnsWindow(window.Name) {
		return
	}

	options := panelRenderOptions(window.Width, window.Height, app.theme, app.keys)

	lines := app.panel.Render(&options)
	for index, line := range lines {
		app.writeStyledLine(window.Y+index, window.Width, line)
	}
}
