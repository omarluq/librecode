package terminal

import (
	"strings"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/extension"
)

func (app *App) applyUIOverrides(layout *runtimeLayout) {
	windows := app.cloneRuntimeWindows(layout)
	for name, override := range app.uiWindowOverrides {
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
		writeTextAt(
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
		writeTextAt(
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
	if window.Width <= 0 || window.Height <= 0 {
		return
	}
	resolved := app.uiStyle(style)
	if window.Width == 1 {
		for row := 0; row < window.Height; row++ {
			writeTextAt(app.frame, window.X, window.Y+row, 1, "│", resolved)
		}
		return
	}
	top := "╭" + strings.Repeat("─", max(0, window.Width-2)) + "╮"
	bottom := "╰" + strings.Repeat("─", max(0, window.Width-2)) + "╯"
	writeTextAt(app.frame, window.X, window.Y, window.Width, top, resolved)
	for row := 1; row < window.Height-1; row++ {
		writeTextAt(app.frame, window.X, window.Y+row, 1, "│", resolved)
		writeTextAt(app.frame, window.X+window.Width-1, window.Y+row, 1, "│", resolved)
	}
	if window.Height > 1 {
		writeTextAt(app.frame, window.X, window.Y+window.Height-1, window.Width, bottom, resolved)
	}
}

func (app *App) drawUIWindowSpans(window *extension.WindowState, drawOp *extension.UIDrawOp) {
	column := drawOp.Col
	for _, span := range drawOp.Spans {
		if column >= window.Width {
			return
		}
		text := terminalTextFit(span.Text, window.Width-column)
		column += writeTextCellsNoFill(
			app.frame,
			window.X+column,
			window.Y+drawOp.Row,
			window.Width-column,
			text,
			app.uiStyle(span.Style),
		)
	}
}

func clearWindow(target cellTarget, window *extension.WindowState) {
	style := tcell.StyleDefault
	for row := 0; row < window.Height; row++ {
		writeLine(target, window.Y+row, window.Width, "", style)
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

func (app *App) showRuntimeCursor(layout *runtimeLayout) {
	if app.screen == nil {
		return
	}
	windows := layout.Windows
	if len(windows) == 0 {
		windows = app.cloneRuntimeWindows(layout)
	}
	if app.uiCursor != nil {
		if window, ok := windows[app.uiCursor.Window]; ok {
			app.screen.ShowCursor(window.X+app.uiCursor.Col, window.Y+app.uiCursor.Row)
			return
		}
	}
	composer, ok := windows[extensionBufferComposer]
	if !ok {
		app.screen.HideCursor()
		return
	}
	app.screen.ShowCursor(composer.X+composer.CursorCol, composer.Y+composer.CursorRow)
}
