package terminal

import (
	"context"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/terminal/extui"
	"github.com/omarluq/librecode/internal/tui"
)

const (
	windowRendererDefault   = "default"
	windowRendererExtension = "extension"
)

func (app *App) screenSize() (width, height int) {
	if app.lastResize != nil {
		return app.lastResize.Size()
	}

	if app.screen != nil {
		return app.screen.Size()
	}

	return defaultTerminalWidth, defaultTerminalHeight
}

func (app *App) prepareScreenForFrame() {
	if app.screen == nil || app.lastResize == nil {
		return
	}

	targetWidth, targetHeight := app.lastResize.Size()

	currentWidth, currentHeight := app.screen.Size()
	if currentWidth != targetWidth || currentHeight != targetHeight {
		app.screen.Show()
	}
}

func (app *App) draw(ctx context.Context) {
	app.prepareScreenForFrame()
	width, height := app.screenSize()

	app.frame = tui.NewCellBuffer(width, height, tcell.StyleDefault)
	if width < 20 || height < 8 {
		app.drawTiny(width, height)
		app.flushFrame()

		return
	}

	if app.needsRuntimeRenderPath() {
		app.drawRuntime(ctx)

		return
	}

	row := 0
	if app.mode == modePanel && app.panel != nil {
		row = app.drawPanel(width, height, row)
	} else {
		row = app.drawMessages(width, height, row)
	}

	app.drawEditorAndFooter(width, height, row)
	app.flushFrame()
}

func (app *App) drawRuntime(ctx context.Context) {
	layout := app.currentRuntimeLayout()
	app.runRenderExtensions(ctx, &layout)

	layout = app.currentRuntimeLayout()
	if app.mode == modePanel && app.panel != nil {
		app.drawPanelWindow(&layout)
	} else {
		app.drawTranscriptWindow(&layout)
	}

	app.drawAutocompleteWindow(&layout)
	app.drawComposerWindow(&layout)
	app.drawStatusWindow(&layout)
	app.applyUIOverrides(&layout)
	app.showRuntimeCursor(&layout)
	app.flushFrame()
}

func (app *App) needsRuntimeRenderPath() bool {
	if app.hasExtensionHandlers(extensionEventRender) ||
		app.extensionUI.Layout != nil ||
		len(app.extensionUI.Windows) > 0 {
		return true
	}

	if len(app.extensionUI.Overrides) > 0 || app.extensionUI.Cursor != nil {
		return true
	}

	_, transcriptOverridden := app.extensionUI.Buffers[extui.BufferTranscript]

	return transcriptOverridden
}

func (app *App) flushFrame() {
	app.applySelectionHighlight()
	app.renderer.Flush(app.frame)
	app.screen.Show()
}

func (app *App) drawTiny(width, height int) {
	message := tui.Truncate("librecode: terminal too small", width)
	tui.WriteCells(app.frame, 0, max(0, height/terminalHalf), width, message, app.theme.style(colorWarning))
}

func (app *App) writeStyledLine(row, width int, line tui.Line) {
	if len(line.Spans) > 0 {
		writeStyled(app.frame, row, width, line)

		return
	}

	if isWorkingIndicatorText(line.Text) {
		_, contentWidth := workingShimmerContentRange(line.Text)
		writeShimmerLineWithVerticalBorders(
			app.frame,
			row,
			width,
			line.Text,
			line.Style,
			shimmerLineOptions{
				borderColor:     app.theme.colors[colorBorderMuted],
				shimmerPosition: app.workingShimmerPosition(contentWidth),
				palette:         app.workingShimmerPalette(),
			},
		)

		return
	}

	writeLineWithVerticalBorders(app.frame, row, width, line.Text, line.Style, app.theme.colors[colorBorderMuted])
}
