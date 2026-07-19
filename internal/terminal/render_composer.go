package terminal

import (
	"slices"
	"strings"

	"github.com/omarluq/librecode/internal/terminal/extui"
	"github.com/omarluq/librecode/internal/tui"
)

func (app *App) drawAutocompleteWindow(layout *extui.Layout) {
	window := layout.Autocomplete
	if !window.Visible || window.Height <= 0 || app.extensionOwnsWindow(window.Name) {
		return
	}

	lines := app.autocompleteLines(window.Width)
	for index, line := range lines {
		writeStyled(app.frame, window.Y+index, window.Width, line)
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

func (app *App) renderComposerEditor(width, bodyRows int) tui.TextAreaRender {
	return app.composerBuffer.Render(width, bodyRows, tui.TextAreaStyles{
		Border: app.theme.style(app.editorBorderColor()),
		Body:   app.theme.style(colorText),
	})
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

		app.writeStyledLine(window.Y+index, window.Width, line)
	}
}

func (app *App) drawEditorAndFooter(width, height, _ int) {
	layout := app.composerLayout(width, height)
	for index, line := range layout.autocompleteLines {
		writeStyled(app.frame, layout.startRow+index, width, line)
	}

	borderStyle := app.theme.style(app.editorBorderColor())
	for index, line := range layout.editor.Lines {
		writeEditorLine(app.frame, layout.editorStart+index, width, line, index, len(layout.editor.Lines), borderStyle)
	}

	for index, line := range layout.footerLines {
		app.writeStyledLine(layout.footerStart+index, width, line)
	}

	if app.transcriptListFocused() || app.agentTaskSummaryFocused() {
		app.screen.HideCursor()

		return
	}

	app.screen.ShowCursor(layout.editor.CursorCol, layout.editorStart+layout.editor.CursorRow)
}

func (app *App) composerReserve(width, height int) int {
	return app.composerLayout(width, height).reserve
}

type composerLayout struct {
	footerLines       []tui.Line
	autocompleteLines []tui.Line
	editor            tui.TextAreaRender
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
	case "medium", "high", "xhigh", "max":
		return colorBorderAccent
	default:
		return colorBorder
	}
}

func (app *App) footerLines(width int) []tui.Line {
	lineTexts := app.defaultStatusLineTexts()

	lines := app.renderAgentTaskSummary(width)
	lines = slices.Grow(lines, len(lineTexts))

	for _, lineText := range lineTexts {
		lines = append(lines, tui.NewLine(app.theme.style(colorDim), tui.Truncate(lineText, width)))
	}

	return lines
}
