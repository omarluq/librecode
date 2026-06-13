package terminal

import (
	"strings"

	"github.com/omarluq/librecode/internal/terminal/extui"
	"github.com/omarluq/librecode/internal/terminal/input"
	"github.com/omarluq/librecode/internal/terminal/rendertext"
)

func (app *App) drawAutocompleteWindow(layout *extui.Layout) {
	window := layout.Autocomplete
	if !window.Visible || window.Height <= 0 || app.extensionOwnsWindow(window.Name) {
		return
	}

	lines := app.autocompleteLines(window.Width)
	for index, line := range lines {
		writeLine(app.frame, window.Y+index, window.Width, line.Text, line.Style)
	}
}

func (app *App) drawComposerWindow(layout *extui.Layout) {
	window := layout.Composer
	if !window.Visible || window.Height <= 0 || app.extensionOwnsWindow(window.Name) {
		return
	}

	editor := app.renderComposerEditor(window.Width, max(1, window.Height-composerBorderRows))

	borderStyle := app.theme.style(app.editorBorderColor())
	for index, line := range editor.Lines {
		writeEditorLine(app.frame, window.Y+index, window.Width, line, index, len(editor.Lines), borderStyle)
	}

	window.CursorRow = editor.CursorRow
	window.CursorCol = editor.CursorCol

	layout.Composer = window
	if layout.Windows != nil {
		layout.Windows[window.Name] = window
	}
}

func (app *App) renderComposerEditor(width, bodyRows int) input.Render {
	return input.RenderEditor(
		[]rune(app.composerBuffer.TextValue()),
		app.composerBuffer.CursorValue(),
		width,
		bodyRows,
		app.theme.style(app.editorBorderColor()),
		app.theme.style(colorText),
		app.composerBuffer.Label,
	)
}

func (app *App) drawStatusWindow(layout *extui.Layout) {
	window := layout.Status
	if !window.Visible || window.Height <= 0 || app.extensionOwnsWindow(window.Name) {
		return
	}

	lines := app.footerLines(window.Width)
	if buffer, ok := app.runtimeBufferOverride(window.Buffer); ok {
		lines = app.renderBufferTextLines(window.Width, buffer.Text, app.theme.style(colorDim))
	}

	for index, line := range lines {
		if index >= window.Height {
			return
		}

		writeLine(app.frame, window.Y+index, window.Width, line.Text, line.Style)
	}
}

func (app *App) drawEditorAndFooter(width, height, _ int) {
	layout := app.composerLayout(width, height)
	for index, line := range layout.autocompleteLines {
		writeLine(app.frame, layout.startRow+index, width, line.Text, line.Style)
	}

	borderStyle := app.theme.style(app.editorBorderColor())
	for index, line := range layout.editor.Lines {
		writeEditorLine(app.frame, layout.editorStart+index, width, line, index, len(layout.editor.Lines), borderStyle)
	}

	for index, line := range layout.footerLines {
		writeLine(app.frame, layout.footerStart+index, width, line.Text, line.Style)
	}

	app.screen.ShowCursor(layout.editor.CursorCol, layout.editorStart+layout.editor.CursorRow)
}

func (app *App) composerReserve(width, height int) int {
	return app.composerLayout(width, height).reserve
}

type composerLayout struct {
	footerLines       []rendertext.Line
	autocompleteLines []rendertext.Line
	editor            input.Render
	startRow          int
	editorStart       int
	footerStart       int
	reserve           int
}

func (app *App) composerLayout(width, height int) composerLayout {
	footerLines := app.footerLines(width)
	autocompleteLines := app.autocompleteLines(width)
	availableRows := height - len(footerLines) - len(autocompleteLines) - composerBorderRows
	maxEditorRows := min(defaultEditorRows, max(minimumComposerHeight, availableRows))
	maxEditorRows = max(minimumComposerHeight, maxEditorRows)
	editor := app.renderComposerEditor(width, maxEditorRows-composerBorderRows)

	reserve := len(footerLines) + len(autocompleteLines) + len(editor.Lines)
	if reserve > height {
		bodyRows := max(1, height-len(footerLines)-len(autocompleteLines)-composerBorderRows)
		editor = app.renderComposerEditor(width, bodyRows)
		reserve = len(footerLines) + len(autocompleteLines) + len(editor.Lines)
	}

	startRow := max(0, height-reserve)
	editorStart := startRow + len(autocompleteLines)
	footerStart := height - len(footerLines)

	return composerLayout{
		editor:            editor,
		footerLines:       footerLines,
		autocompleteLines: autocompleteLines,
		startRow:          startRow,
		editorStart:       editorStart,
		footerStart:       footerStart,
		reserve:           reserve,
	}
}

func (app *App) editorBorderColor() colorToken {
	if strings.HasPrefix(strings.TrimSpace(app.composerBuffer.TextValue()), "!") {
		return colorBashMode
	}

	switch app.currentThinkingLevel() {
	case "minimal", "low":
		return colorBorderMuted
	case "medium", "high", "xhigh":
		return colorBorderAccent
	default:
		return colorBorder
	}
}

func (app *App) footerLines(width int) []rendertext.Line {
	lineTexts := app.defaultStatusLineTexts()

	lines := make([]rendertext.Line, 0, len(lineTexts))
	for _, lineText := range lineTexts {
		lines = append(lines, rendertext.NewLine(app.theme.style(colorDim), rendertext.Truncate(lineText, width)))
	}

	return lines
}
