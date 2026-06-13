package terminal

import (
	"github.com/omarluq/librecode/internal/terminal/extui"
	"github.com/omarluq/librecode/internal/tui"
)

func (app *App) drawMessages(width, height, row int) int {
	if app.showWelcomeOnly() {
		return app.drawWelcomeOnly(width, height, row)
	}

	availableRows := max(1, height-row-app.composerReserve(width, height))
	app.transcript.LastMaxRows = availableRows

	lines := app.messageLines(width, availableRows)
	for _, line := range lines {
		app.writeStyledLine(row, width, line)
		row++
	}

	return row
}

func (app *App) drawTranscriptWindow(layout *extui.Layout) {
	window := layout.Transcript
	if !window.Visible || window.Height <= 0 || app.extensionOwnsWindow(window.Name) {
		return
	}

	if buffer, ok := app.runtimeBufferOverride(window.Buffer); ok {
		app.drawRuntimeTextBuffer(&window, &buffer, app.theme.style(colorText))

		return
	}

	if app.showWelcomeOnly() {
		app.drawWelcomeOnly(window.Width, window.Height, window.Y)

		return
	}

	lines := app.messageLines(window.Width, window.Height)
	for index, line := range lines {
		app.writeStyledLine(window.Y+index, window.Width, line)
	}
}

func (app *App) messageLines(width, maxRows int) []tui.Line {
	app.transcript.LastMaxRows = maxRows

	dynamicGroups := app.dynamicMessageLineGroups(width)
	if maxRows < 0 {
		return app.allMessageLines(width, dynamicGroups)
	}

	if app.scrollOffset == 0 {
		return app.bottomMessageLines(width, maxRows, dynamicGroups)
	}

	return app.scrolledMessageLines(width, maxRows, dynamicGroups)
}

func (app *App) cachedMessageLines(width, index int) []tui.Line {
	return app.transcript.LineCache.lines(app, width, index)
}

func (app *App) currentLineCacheState(width int) messageLineCacheState {
	return messageLineCacheState{
		ThemeName:     app.theme.name,
		Width:         width,
		HideThinking:  app.hideThinking,
		ToolsExpanded: app.toolsExpanded,
	}
}

func (app *App) rebuildMessageRowPrefixSums(width int) {
	app.transcript.LineCache.rebuildPrefixes(app, width)
}

func (app *App) warmMessageLineCache() {
	for !app.transcript.LineCache.warm {
		if !app.warmMessageLineCacheStep() {
			return
		}
	}
}

func (app *App) warmMessageLineCacheStep() bool {
	return app.transcript.LineCache.warmStep(app)
}
