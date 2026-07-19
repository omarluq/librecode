package terminal

import (
	"strings"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/terminal/extui"
	"github.com/omarluq/librecode/internal/tui"
)

func (app *App) applyUIOverrides(layout *extui.Layout) {
	windows := app.cloneRuntimeWindows(layout)
	for name, override := range app.extensionUI.Overrides {
		window, ok := windows[name]
		if !ok || !window.Visible {
			continue
		}

		if override.Reset {
			clearWindow(app.frame, &window)
		}

		for index := range override.DrawOps {
			drawOp := &override.DrawOps[index]
			app.drawUIWindowText(&window, drawOp)
		}
	}
}

func (app *App) drawUIWindowText(window *extension.WindowState, drawOp *extension.UIDrawOp) {
	switch strings.ToLower(strings.TrimSpace(drawOp.Kind)) {
	case extension.UIDrawKindBox:
		app.drawUIWindowBox(window, drawOp.Style)
	case extension.UIDrawKindClear:
		app.drawUIWindowClear(window, drawOp)
	case extension.UIDrawKindSpans:
		app.drawUIWindowSpans(window, drawOp)
	default:
		style := app.uiStyle(drawOp.Style)
		tui.WriteCells(
			app.frame,
			window.X+drawOp.Col,
			window.Y+drawOp.Row,
			window.Width-drawOp.Col,
			drawOp.Text,
			style,
		)
	}
}

func (app *App) drawUIWindowClear(window *extension.WindowState, drawOp *extension.UIDrawOp) {
	startRow := min(max(drawOp.Row, 0), window.Height)
	startCol := min(max(drawOp.Col, 0), window.Width)

	height := drawOp.Height
	if height <= 0 {
		height = window.Height - startRow
	}

	width := drawOp.Width
	if width <= 0 {
		width = window.Width - startCol
	}

	endRow := min(max(startRow+height, startRow), window.Height)
	endCol := min(max(startCol+width, startCol), window.Width)

	style := app.uiStyle(drawOp.Style)
	for row := startRow; row < endRow; row++ {
		tui.WriteCells(
			app.frame,
			window.X+startCol,
			window.Y+row,
			endCol-startCol,
			"",
			style,
		)
	}
}

func (app *App) drawUIWindowBox(window *extension.WindowState, style extension.UIStyle) {
	box := tui.NewBox("")
	box.Style = app.uiStyle(style)
	box.Draw(app.frame, tui.Rect{X: window.X, Y: window.Y, Width: window.Width, Height: window.Height})
}

func (app *App) drawUIWindowSpans(window *extension.WindowState, drawOp *extension.UIDrawOp) {
	column := drawOp.Col
	for _, span := range drawOp.Spans {
		if column >= window.Width {
			return
		}

		text := tui.Fit(span.Text, window.Width-column)
		column += tui.WriteCellsNoFill(
			app.frame,
			window.X+column,
			window.Y+drawOp.Row,
			window.Width-column,
			text,
			app.uiStyle(span.Style),
		)
	}
}

func clearWindow(target tui.ContentSetter, window *extension.WindowState) {
	style := tcell.StyleDefault
	for row := 0; row < window.Height; row++ {
		tui.WriteCells(target, window.X, window.Y+row, window.Width, "", style)
	}
}

func (app *App) uiStyle(style extension.UIStyle) tcell.Style {
	resolved := tcell.StyleDefault
	if style.FG != "" {
		resolved = resolved.Foreground(app.namedUIColor(style.FG))
	}

	if style.BG != "" {
		resolved = resolved.Background(app.namedUIColor(style.BG))
	}

	if style.Bold {
		resolved = resolved.Bold(true)
	}

	if style.Italic {
		resolved = resolved.Italic(true)
	}

	return resolved
}

func (app *App) showRuntimeCursor(layout *extui.Layout) {
	if app.screen == nil {
		return
	}

	windows := layout.Windows
	if len(windows) == 0 {
		windows = app.cloneRuntimeWindows(layout)
	}

	if app.extensionUI.Cursor != nil {
		if window, ok := windows[app.extensionUI.Cursor.Window]; ok {
			app.screen.ShowCursor(window.X+app.extensionUI.Cursor.Col, window.Y+app.extensionUI.Cursor.Row)

			return
		}
	}

	if app.transcriptListFocused() || app.agentTaskSummaryFocused() {
		app.screen.HideCursor()

		return
	}

	composer, ok := windows[extui.BufferComposer]
	if !ok {
		app.screen.HideCursor()

		return
	}

	app.screen.ShowCursor(composer.X+composer.CursorCol, composer.Y+composer.CursorRow)
}
