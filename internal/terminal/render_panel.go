package terminal

func (app *App) drawPanel(width, height, row int) int {
	availableHeight := max(1, height-row-app.composerReserve(width, height))
	lines := app.panel.render(width, availableHeight, app.theme, app.keys)
	for _, line := range lines {
		app.writeStyledLine(row, width, line)
		row++
	}

	return row
}

func (app *App) drawPanelWindow(layout *runtimeLayout) {
	window := layout.Transcript
	if !window.Visible || window.Height <= 0 || app.extensionOwnsWindow(window.Name) {
		return
	}
	lines := app.panel.render(window.Width, window.Height, app.theme, app.keys)
	for index, line := range lines {
		app.writeStyledLine(window.Y+index, window.Width, line)
	}
}
