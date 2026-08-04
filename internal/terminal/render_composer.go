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
	chips := app.visibleAttachmentChipLines(width, max(0, bodyRows-1))
	textRows := max(1, bodyRows-len(chips))

	rendered := app.composerBuffer.Render(width, textRows, tui.TextAreaStyles{
		Border: app.theme.style(app.editorBorderColor()),
		Body:   app.theme.style(colorText),
	})
	if len(chips) == 0 {
		return rendered
	}

	const afterTopBorder = 1

	lines := make([]tui.Line, 0, len(rendered.Lines)+len(chips))
	lines = append(lines, rendered.Lines[:afterTopBorder]...)
	lines = append(lines, chips...)
	lines = append(lines, rendered.Lines[afterTopBorder:]...)
	rendered.Lines = lines
	rendered.CursorRow += len(chips)

	return rendered
}

func (app *App) attachmentChipLines(width int) []tui.Line {
	lines := make([]tui.Line, 0, len(app.composerImages))
	for _, item := range app.composerImages {
		summary := attachmentSummary{
			Name: item.Name, MIMEType: item.MIMEType, Width: item.Width,
			Height: item.Height, Size: len(item.Data),
		}
		text := "  " + attachmentSummaryText(summary)
		lines = append(lines, tui.NewLine(app.theme.style(colorDim), tui.Truncate(text, width)))
	}

	return lines
}

func (app *App) visibleAttachmentChipLines(width, limit int) []tui.Line {
	chips := app.attachmentChipLines(width)
	if limit <= 0 || len(chips) == 0 {
		return nil
	}

	if len(chips) <= limit {
		return chips
	}

	visible := max(0, limit-1)
	lines := chips[:visible]
	overflow := "  … " + tui.Int(len(chips)-visible) + " more attachments"

	return append(lines, tui.NewLine(app.theme.style(colorDim), tui.Truncate(overflow, width)))
}

func formatByteSize(size int) string {
	const (
		kibibyte = 1024
		mebibyte = kibibyte * kibibyte
	)

	if size >= mebibyte {
		return tui.Int((size+mebibyte-1)/mebibyte) + " MiB"
	}

	return tui.Int((size+kibibyte-1)/kibibyte) + " KiB"
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

	if len(layout.editor.Lines) == 0 || app.transcriptListFocused() || app.agentTaskSummaryFocused() {
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
	}

	footerLines, autocompleteLines, editor = fitComposerRows(
		height, footerLines, autocompleteLines, editor,
	)
	reserve = len(footerLines) + len(autocompleteLines) + len(editor.Lines)
	startRow := max(0, height-reserve)
	editorStart := startRow + len(autocompleteLines)
	footerStart := editorStart + len(editor.Lines)

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

func fitComposerRows(
	height int,
	footerLines, autocompleteLines []tui.Line,
	editor tui.TextAreaRender,
) (footer, autocomplete []tui.Line, fittedEditor tui.TextAreaRender) {
	height = max(0, height)
	if len(footerLines) > height {
		footerLines = footerLines[:height]
	}

	remaining := height - len(footerLines)
	if len(autocompleteLines) > remaining {
		autocompleteLines = autocompleteLines[:remaining]
	}

	remaining -= len(autocompleteLines)
	if len(editor.Lines) > remaining {
		editor.Lines = editor.Lines[:remaining]
	}

	if len(editor.Lines) == 0 {
		editor.CursorRow = 0
		editor.CursorCol = 0
	} else {
		editor.CursorRow = min(editor.CursorRow, len(editor.Lines)-1)
	}

	return footerLines, autocompleteLines, editor
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
