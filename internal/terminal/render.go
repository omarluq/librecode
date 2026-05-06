package terminal

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/database"
)

func (app *App) draw() {
	app.screen.Clear()
	width, height := app.screen.Size()
	if width < 20 || height < 8 {
		app.drawTiny(width, height)
		app.screen.Show()
		return
	}

	row := 0
	row = app.drawHeader(width, row)
	if app.mode == modePanel && app.panel != nil {
		row = app.drawPanel(width, height, row)
	} else {
		row = app.drawMessages(width, height, row)
	}
	app.drawEditorAndFooter(width, height, row)
	app.screen.Show()
}

func (app *App) drawTiny(width, height int) {
	message := truncateText("librecode: terminal too small", width)
	writeLine(app.screen, max(0, height/2), width, message, app.theme.style(colorWarning))
}

func (app *App) drawHeader(width, row int) int {
	title := " librecode "
	writeLine(app.screen, row, width, padRight(title, width), app.theme.background(colorToolPendingBg).Bold(true))
	row++
	shortcuts := []string{
		"/hotkeys",
		app.keys.hint(actionModelSelect) + " model",
		app.keys.hint(actionThinkingCycle) + " thinking",
		app.keys.hint(actionToolsExpand) + " tools",
		"/settings",
		"/tree",
	}
	writeLine(app.screen, row, width, strings.Join(shortcuts, " • "), app.theme.style(colorDim))
	row++
	if app.statusMessage != "" {
		writeLine(app.screen, row, width, app.statusMessage, app.theme.style(colorAccent))
		row++
	}

	return row + 1
}

func (app *App) drawPanel(width, height, row int) int {
	availableHeight := max(1, height-row-footerReserve())
	lines := app.panel.render(width, availableHeight, app.theme, app.keys)
	for _, line := range lines {
		writeLine(app.screen, row, width, line.Text, line.Style)
		row++
	}

	return row
}

func (app *App) drawMessages(width, height, row int) int {
	availableRows := max(1, height-row-footerReserve())
	lines := app.messageLines(width, availableRows)
	for _, line := range lines {
		writeLine(app.screen, row, width, line.Text, line.Style)
		row++
	}

	return row
}

func (app *App) messageLines(width, maxRows int) []styledLine {
	lines := []styledLine{}
	for _, message := range app.messages {
		lines = append(lines, app.renderMessage(width, message)...)
	}
	if app.working {
		lines = append(lines, styledLine{Style: app.theme.style(colorAccent), Text: "⠋ working…"})
	}
	if len(app.queuedMessages) > 0 {
		queueText := fmt.Sprintf("queued follow-ups: %d", len(app.queuedMessages))
		lines = append(lines, styledLine{Style: app.theme.style(colorWarning), Text: queueText})
	}

	return safeSlice(lines, maxRows)
}

func (app *App) renderMessage(width int, message chatMessage) []styledLine {
	switch message.Role {
	case database.RoleUser:
		return app.renderUserMessage(width, message.Content)
	case database.RoleAssistant:
		return app.renderAssistantMessage(width, message.Content)
	case database.RoleToolResult, database.RoleBashExecution:
		return app.renderToolMessage(width, message)
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
	lines := make([]styledLine, 0, len(wrapped)+1)
	lines = append(lines, styledLine{Style: app.theme.style(colorDim), Text: ""})
	for _, line := range wrapped {
		text := "  " + padRight(line, innerWidth) + "  "
		lines = append(lines, styledLine{Style: app.theme.background(colorUserMessageBg), Text: text})
	}

	return lines
}

func (app *App) renderAssistantMessage(width int, content string) []styledLine {
	innerWidth := max(1, width-2)
	wrapped := wrapText(content, innerWidth)
	lines := make([]styledLine, 0, len(wrapped)+1)
	lines = append(lines, styledLine{Style: app.theme.style(colorDim), Text: ""})
	for _, line := range wrapped {
		lines = append(lines, styledLine{Style: app.theme.style(colorText), Text: " " + line})
	}

	return lines
}

func (app *App) renderToolMessage(width int, message chatMessage) []styledLine {
	if !app.toolsExpanded {
		label := string(message.Role) + " " + app.keys.hint(actionToolsExpand) + " expand"
		return []styledLine{{Style: app.theme.background(colorToolSuccessBg), Text: padRight(label, width)}}
	}

	return boxedLines(width, string(message.Role), message.Content, app.theme.background(colorToolSuccessBg))
}

func (app *App) renderCustomMessage(width int, content string) []styledLine {
	return boxedLines(width, "system", content, app.theme.background(colorCustomMessageBg))
}

func (app *App) renderSummaryMessage(width int, message chatMessage) []styledLine {
	return boxedLines(width, string(message.Role), message.Content, app.theme.style(colorMuted))
}

func boxedLines(width int, label, content string, style tcell.Style) []styledLine {
	innerWidth := max(1, width-4)
	wrapped := wrapText(content, innerWidth)
	lines := make([]styledLine, 0, len(wrapped)+2)
	lines = append(lines,
		styledLine{Style: style, Text: ""},
		styledLine{Style: style.Bold(true), Text: padRight("  ["+label+"]", width)},
	)
	for _, line := range wrapped {
		lines = append(lines, styledLine{Style: style, Text: "  " + padRight(line, innerWidth) + "  "})
	}

	return lines
}

func (app *App) drawEditorAndFooter(width, height, _ int) {
	footerLines := app.footerLines(width)
	editorRows := min(defaultEditorRows, max(3, height-len(footerLines)-2))
	editorRender := app.editor.render(width, editorRows-2, app.theme, app.editorBorderColor())
	startRow := max(0, height-len(footerLines)-len(editorRender.Lines))
	for index, line := range editorRender.Lines {
		writeLine(app.screen, startRow+index, width, line.Text, line.Style)
	}
	footerStart := height - len(footerLines)
	for index, line := range footerLines {
		writeLine(app.screen, footerStart+index, width, line.Text, line.Style)
	}
	app.screen.ShowCursor(editorRender.CursorCol, startRow+editorRender.CursorRow)
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

	return []styledLine{
		{Style: app.theme.style(colorDim), Text: truncateText(pathLine, width)},
		{Style: app.theme.style(colorDim), Text: truncateText(statusLine, width)},
	}
}

func footerReserve() int {
	return defaultEditorRows + 3
}

func writeLine(screen tcell.Screen, row, width int, text string, style tcell.Style) {
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
