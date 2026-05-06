package terminal

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/database"
)

func (app *App) draw() {
	width, height := app.screen.Size()
	app.frame = newCellBuffer(width, height, tcell.StyleDefault)
	if width < 20 || height < 8 {
		app.drawTiny(width, height)
		app.flushFrame()
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

func (app *App) flushFrame() {
	app.renderer.flush(app.frame)
	app.screen.Show()
}

func (app *App) drawTiny(width, height int) {
	message := truncateText("librecode: terminal too small", width)
	writeLine(app.frame, max(0, height/2), width, message, app.theme.style(colorWarning))
}

func (app *App) drawPanel(width, height, row int) int {
	availableHeight := max(1, height-row-footerReserve())
	lines := app.panel.render(width, availableHeight, app.theme, app.keys)
	for _, line := range lines {
		app.writeStyledLine(row, width, line)
		row++
	}

	return row
}

func (app *App) drawMessages(width, height, row int) int {
	if app.showWelcomeOnly() {
		return app.drawWelcomeOnly(width, height, row)
	}
	availableRows := max(1, height-row-footerReserve())
	lines := app.messageLines(width, availableRows)
	for _, line := range lines {
		app.writeStyledLine(row, width, line)
		row++
	}

	return row
}

func (app *App) messageLines(width, maxRows int) []styledLine {
	lines := []styledLine{}
	for _, message := range app.messages {
		lines = append(lines, app.renderMessage(width, message)...)
	}
	if app.streamingText != "" {
		lines = append(lines, app.renderAssistantMessage(width, app.streamingText)...)
	}
	if app.working {
		lines = append(lines, styledLine{Style: app.theme.style(colorAccent), Text: app.workingIndicator()})
	}
	if len(app.queuedMessages) > 0 {
		queueText := fmt.Sprintf("queued follow-ups: %d", len(app.queuedMessages))
		lines = append(lines, styledLine{Style: app.theme.style(colorWarning), Text: queueText})
	}

	return app.visibleMessageLines(lines, maxRows)
}

func (app *App) renderMessage(width int, message chatMessage) []styledLine {
	switch message.Role {
	case database.RoleUser:
		return app.renderUserMessage(width, message.Content)
	case database.RoleAssistant:
		return app.renderAssistantMessage(width, message.Content)
	case database.RoleToolResult, database.RoleBashExecution:
		return app.renderToolMessage(width, message)
	case database.RoleThinking:
		return app.renderThinkingMessage(width, message)
	case database.RoleCustom:
		return app.renderCustomMessage(width, message.Content)
	case database.RoleBranchSummary, database.RoleCompactionSummary:
		return app.renderSummaryMessage(width, message)
	}

	return app.renderCustomMessage(width, message.Content)
}

func (app *App) renderUserMessage(width int, content string) []styledLine {
	innerWidth := max(1, width-4)
	wrapped := wrapText(content, innerWidth)
	lines := make([]styledLine, 0, len(wrapped)+4)
	lines = append(lines,
		styledLine{Style: app.theme.style(colorDim), Text: ""},
		styledLine{Style: app.theme.background(colorUserMessageBg), Text: padRight("", width)},
	)
	for _, line := range wrapped {
		text := "  " + padRight(line, innerWidth) + "  "
		lines = append(lines, styledLine{Style: app.theme.background(colorUserMessageBg), Text: text})
	}
	lines = append(lines,
		styledLine{Style: app.theme.background(colorUserMessageBg), Text: padRight("", width)},
		styledLine{Style: app.theme.style(colorDim), Text: ""},
	)

	return lines
}

func (app *App) renderAssistantMessage(width int, content string) []styledLine {
	markdownLines := app.renderMarkdown(strings.TrimSpace(content), width)
	lines := make([]styledLine, 0, len(markdownLines)+2)
	lines = append(lines, styledLine{Style: app.theme.style(colorDim), Text: ""})
	lines = append(lines, markdownLines...)
	lines = append(lines, styledLine{Style: app.theme.style(colorDim), Text: ""})

	return lines
}

func (app *App) renderToolMessage(width int, message chatMessage) []styledLine {
	return app.renderToolBlock(width, message)
}

func (app *App) renderThinkingMessage(width int, message chatMessage) []styledLine {
	if app.hideThinking {
		return []styledLine{
			{Style: tcell.StyleDefault, Text: ""},
			{Style: app.theme.style(colorThinkingText).Italic(true), Text: " thinking…"},
			{Style: tcell.StyleDefault, Text: ""},
		}
	}

	style := app.theme.style(colorThinkingText).Italic(true)
	lines := []styledLine{{Style: style.Bold(true), Text: boxTop(width, "thinking")}}
	for _, line := range app.renderMarkdown(strings.TrimSpace(message.Content), max(1, width-4)) {
		lines = append(lines, styledLine{Style: style, Text: boxedBodyLine(width, line.Text)})
	}
	lines = append(lines,
		styledLine{Style: style.Bold(true), Text: boxBottom(width)},
		styledLine{Style: app.theme.style(colorDim), Text: ""},
	)

	return lines
}

func (app *App) renderCustomMessage(width int, content string) []styledLine {
	if isWelcomeMessage(content) {
		return app.renderWelcomeMessage(width, content)
	}

	return boxedLines(width, "system", content, app.theme.background(colorCustomMessageBg))
}

func (app *App) renderSummaryMessage(width int, message chatMessage) []styledLine {
	return boxedLines(width, string(message.Role), message.Content, app.theme.style(colorMuted))
}

func boxedLines(width int, label, content string, style tcell.Style) []styledLine {
	innerWidth := max(1, width-4)
	wrapped := wrapText(content, innerWidth)
	lines := make([]styledLine, 0, len(wrapped)+5)
	lines = append(lines,
		styledLine{Style: tcell.StyleDefault, Text: ""},
		styledLine{Style: style, Text: padRight("", width)},
		styledLine{Style: style.Bold(true), Text: padRight("  ["+label+"]", width)},
	)
	for _, line := range wrapped {
		lines = append(lines, styledLine{Style: style, Text: "  " + padRight(line, innerWidth) + "  "})
	}
	lines = append(lines,
		styledLine{Style: style, Text: padRight("", width)},
		styledLine{Style: tcell.StyleDefault, Text: ""},
	)

	return lines
}

func (app *App) drawEditorAndFooter(width, height, _ int) {
	footerLines := app.footerLines(width)
	autocompleteLines := app.autocompleteLines(width)
	editorRows := min(defaultEditorRows, max(3, height-len(footerLines)-len(autocompleteLines)-2))
	editorRender := app.editor.render(width, editorRows-2, app.theme, app.editorBorderColor())
	startRow := max(0, height-len(footerLines)-len(autocompleteLines)-len(editorRender.Lines))
	for index, line := range autocompleteLines {
		writeLine(app.frame, startRow+index, width, line.Text, line.Style)
	}
	editorStart := startRow + len(autocompleteLines)
	borderStyle := app.theme.style(app.editorBorderColor())
	for index, line := range editorRender.Lines {
		writeEditorLine(app.frame, editorStart+index, width, line, index, len(editorRender.Lines), borderStyle)
	}
	footerStart := height - len(footerLines)
	for index, line := range footerLines {
		writeLine(app.frame, footerStart+index, width, line.Text, line.Style)
	}
	app.screen.ShowCursor(editorRender.CursorCol, editorStart+editorRender.CursorRow)
}

func (app *App) editorBorderColor() colorToken {
	if strings.HasPrefix(strings.TrimSpace(app.editor.text()), "!") {
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

func (app *App) footerLines(width int) []styledLine {
	pathLine := app.cwd
	if app.sessionID != "" {
		pathLine += " • " + app.sessionID
	}
	stats := "↑0 ↓0 R0 W0 0%/0"
	modelText := modelLabel(app.currentProvider(), app.currentModel())
	if app.currentThinkingLevel() != "" {
		modelText += " • " + app.currentThinkingLevel()
	}
	padding := max(1, width-runeLen(stats)-runeLen(modelText))
	statusLine := stats + strings.Repeat(" ", padding) + modelText
	if len(app.queuedMessages) > 0 {
		statusLine = "queued " + intText(len(app.queuedMessages)) + " • " + statusLine
	}
	if app.statusMessage != "" {
		statusLine = app.statusMessage + " • " + statusLine
	}

	return []styledLine{
		{Style: app.theme.style(colorDim), Text: truncateText(pathLine, width)},
		{Style: app.theme.style(colorDim), Text: truncateText(statusLine, width)},
	}
}

func footerReserve() int {
	return defaultEditorRows + 3
}

func (app *App) writeStyledLine(row, width int, line styledLine) {
	writeLineWithVerticalBorders(app.frame, row, width, line.Text, line.Style, app.theme.colors[colorBorderMuted])
}

func writeLine(screen cellTarget, row, width int, text string, style tcell.Style) {
	if row < 0 {
		return
	}
	line := []rune(truncateText(text, width))
	for index := 0; index < width; index++ {
		value := ' '
		if index < len(line) {
			value = line[index]
		}
		screen.SetContent(index, row, value, nil, style)
	}
}

func writeLineWithVerticalBorders(
	screen cellTarget,
	row int,
	width int,
	text string,
	style tcell.Style,
	borderColor tcell.Color,
) {
	if row < 0 {
		return
	}
	line := []rune(truncateText(text, width))
	for index := 0; index < width; index++ {
		value := ' '
		if index < len(line) {
			value = line[index]
		}
		cellStyle := style
		if isVerticalBorderCell(value, index, width) {
			cellStyle = style.Foreground(borderColor)
		}
		screen.SetContent(index, row, value, nil, cellStyle)
	}
}

func isVerticalBorderCell(value rune, index, width int) bool {
	return value == '│' && (index == 0 || index == width-1)
}

func writeEditorLine(
	screen cellTarget,
	row int,
	width int,
	line styledLine,
	lineIndex int,
	lineCount int,
	borderStyle tcell.Style,
) {
	if lineIndex == 0 || lineIndex == lineCount-1 {
		writeLine(screen, row, width, line.Text, line.Style)
		return
	}
	if row < 0 {
		return
	}
	bodyRunes := []rune(truncateText(line.Text, width))
	for index := 0; index < width; index++ {
		value := ' '
		if index < len(bodyRunes) {
			value = bodyRunes[index]
		}
		style := line.Style
		if index < 2 || index >= max(0, width-2) {
			style = borderStyle
		}
		screen.SetContent(index, row, value, nil, style)
	}
}
