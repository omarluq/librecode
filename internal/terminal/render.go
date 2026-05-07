package terminal

import (
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
	availableHeight := max(1, height-row-app.composerReserve(width, height))
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
	availableRows := max(1, height-row-app.composerReserve(width, height))
	lines := app.messageLines(width, availableRows)
	for _, line := range lines {
		app.writeStyledLine(row, width, line)
		row++
	}

	return row
}

func (app *App) messageLines(width, maxRows int) []styledLine {
	return app.visibleMessageLineGroups(app.messageLineGroups(width), maxRows)
}

func (app *App) messageLineGroups(width int) [][]styledLine {
	groups := make([][]styledLine, 0, len(app.messages)+len(app.streamingBlocks)+3)
	for index := range app.messages {
		groups = append(groups, app.cachedMessageLines(width, index))
	}
	if len(app.streamingBlocks) > 0 {
		for index := range app.streamingBlocks {
			groups = append(groups, app.cachedStreamingBlockLines(width, index))
		}
	} else {
		if app.streamingThinkingText != "" {
			groups = append(groups, app.renderStreamingThinkingMessage(width, app.streamingThinkingText))
		}
		if app.streamingText != "" {
			groups = append(groups, app.renderStreamingMessage(width, app.streamingText))
		}
	}
	if app.working {
		groups = append(groups, []styledLine{{Style: app.theme.style(colorAccent), Text: app.workingIndicator()}})
	}
	if len(app.queuedMessages) > 0 {
		groups = append(groups, app.renderQueuedMessages(width))
	}

	return groups
}

func (app *App) cachedMessageLines(width, index int) []styledLine {
	app.ensureMessageLineCache(width)
	if index < len(app.messageLineCache) && app.messageLineCache[index].Valid {
		return app.messageLineCache[index].Lines
	}
	lines := app.renderMessage(width, app.messages[index])
	if index >= len(app.messageLineCache) {
		return lines
	}
	app.messageLineCache[index] = cachedRenderedMessage{Lines: lines, Valid: true}

	return lines
}

func (app *App) ensureMessageLineCache(width int) {
	app.ensureLineCache(width, len(app.messages), &app.messageLineCache, &app.messageLineCacheState)
}

func (app *App) ensureLineCache(
	width int,
	targetLength int,
	cache *[]cachedRenderedMessage,
	cacheState *messageLineCacheState,
) {
	state := app.currentLineCacheState(width)
	if *cacheState != state {
		*cache = nil
		*cacheState = state
	}
	if len(*cache) > targetLength {
		*cache = (*cache)[:targetLength]
	}
	for len(*cache) < targetLength {
		*cache = append(*cache, emptyCachedRenderedMessage())
	}
}

func (app *App) currentLineCacheState(width int) messageLineCacheState {
	return messageLineCacheState{
		ThemeName:     app.theme.name,
		Width:         width,
		HideThinking:  app.hideThinking,
		ToolsExpanded: app.toolsExpanded,
	}
}

func (app *App) cachedStreamingBlockLines(width, index int) []styledLine {
	app.ensureStreamingBlockLineCache(width)
	if index < len(app.streamingBlockLineCache) && app.streamingBlockLineCache[index].Valid {
		return app.streamingBlockLineCache[index].Lines
	}
	lines := app.renderStreamingBlockMessage(width, app.streamingBlocks[index])
	if index >= len(app.streamingBlockLineCache) {
		return lines
	}
	app.streamingBlockLineCache[index] = cachedRenderedMessage{Lines: lines, Valid: true}

	return lines
}

func (app *App) ensureStreamingBlockLineCache(width int) {
	app.ensureLineCache(
		width,
		len(app.streamingBlocks),
		&app.streamingBlockLineCache,
		&app.streamingBlockLineCacheState,
	)
}

func (app *App) visibleMessageLineGroups(groups [][]styledLine, maxRows int) []styledLine {
	totalRows := 0
	for _, group := range groups {
		totalRows += len(group)
	}
	if maxRows < 0 || totalRows <= maxRows {
		app.scrollOffset = 0
		return flattenStyledLineGroups(groups, totalRows)
	}
	maxOffset := max(0, totalRows-maxRows)
	app.scrollOffset = min(app.scrollOffset, maxOffset)
	end := totalRows - app.scrollOffset
	start := max(0, end-maxRows)

	return sliceStyledLineGroups(groups, start, end)
}

func flattenStyledLineGroups(groups [][]styledLine, totalRows int) []styledLine {
	lines := make([]styledLine, 0, totalRows)
	for _, group := range groups {
		lines = append(lines, group...)
	}

	return lines
}

func sliceStyledLineGroups(groups [][]styledLine, start, end int) []styledLine {
	lines := make([]styledLine, 0, max(0, end-start))
	offset := 0
	for _, group := range groups {
		nextOffset := offset + len(group)
		if nextOffset > start && offset < end {
			groupStart := max(0, start-offset)
			groupEnd := min(len(group), end-offset)
			lines = append(lines, group[groupStart:groupEnd]...)
		}
		offset = nextOffset
		if offset >= end {
			break
		}
	}

	return lines
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

func (app *App) renderQueuedMessages(width int) []styledLine {
	style := app.theme.background(colorUserMessageBg).Foreground(app.theme.colors[colorMuted])
	innerWidth := max(1, width-4)
	lines := []styledLine{{Style: app.theme.style(colorDim), Text: ""}}
	for index, message := range app.queuedMessages {
		header := "queued follow-up " + intText(index+1)
		lines = append(lines, styledLine{Style: style.Bold(true), Text: "  " + padRight(header, innerWidth) + "  "})
		for _, line := range wrapText(message, innerWidth) {
			lines = append(lines, styledLine{Style: style, Text: "  " + padRight(line, innerWidth) + "  "})
		}
	}
	lines = append(lines, styledLine{Style: app.theme.style(colorDim), Text: ""})

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

func (app *App) renderStreamingMessage(width int, content string) []styledLine {
	wrapped := wrapText(strings.TrimSpace(content), width)
	style := app.theme.style(colorText)
	lines := make([]styledLine, 0, len(wrapped)+2)
	lines = append(lines, styledLine{Style: app.theme.style(colorDim), Text: ""})
	for _, line := range wrapped {
		lines = append(lines, styledLine{Style: style, Text: line})
	}
	lines = append(lines, styledLine{Style: app.theme.style(colorDim), Text: ""})

	return lines
}

func (app *App) renderStreamingThinkingMessage(width int, content string) []styledLine {
	return app.renderThinkingMessage(width, newChatMessage(database.RoleThinking, content))
}

func (app *App) renderStreamingBlockMessage(width int, message chatMessage) []styledLine {
	switch message.Role {
	case database.RoleAssistant:
		return app.renderStreamingMessage(width, message.Content)
	case database.RoleThinking:
		return app.renderStreamingThinkingMessage(width, message.Content)
	case database.RoleToolResult, database.RoleBashExecution:
		return app.renderToolMessage(width, message)
	case database.RoleUser,
		database.RoleCustom,
		database.RoleBranchSummary,
		database.RoleCompactionSummary:
		return app.renderMessage(width, message)
	}

	return app.renderMessage(width, message)
}

func (app *App) renderToolMessage(width int, message chatMessage) []styledLine {
	return app.renderToolBlock(width, message)
}

func (app *App) renderThinkingMessage(width int, message chatMessage) []styledLine {
	style := app.theme.style(colorDim).Italic(true)
	if app.hideThinking {
		return []styledLine{
			{Style: tcell.StyleDefault, Text: ""},
			{Style: style, Text: "thinking…"},
			{Style: tcell.StyleDefault, Text: ""},
		}
	}

	markdownLines := app.renderMarkdown(strings.TrimSpace(message.Content), width)
	lines := make([]styledLine, 0, len(markdownLines)+3)
	lines = append(lines,
		styledLine{Style: tcell.StyleDefault, Text: ""},
		styledLine{Style: style.Bold(true), Text: settingThinking},
	)
	for _, line := range markdownLines {
		lines = append(lines, styledLine{Style: style, Text: line.Text})
	}
	lines = append(lines, styledLine{Style: app.theme.style(colorDim), Text: ""})

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
	footerLines       []styledLine
	autocompleteLines []styledLine
	editor            editorRender
	startRow          int
	editorStart       int
	footerStart       int
	reserve           int
}

func (app *App) composerLayout(width, height int) composerLayout {
	footerLines := app.footerLines(width)
	autocompleteLines := app.autocompleteLines(width)
	editorRows := min(defaultEditorRows, max(3, height-len(footerLines)-len(autocompleteLines)-2))
	editor := app.editor.render(width, editorRows-2, app.theme, app.editorBorderColor())
	reserve := len(footerLines) + len(autocompleteLines) + len(editor.Lines)
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
	modelText := modelLabel(app.currentProvider(), app.currentModel())
	if app.currentThinkingLevel() != "" {
		modelText += " • " + app.currentThinkingLevel()
	}
	if label := app.composerFooterLabel(); label != "" {
		modelText = label + " • " + modelText
	}

	return []styledLine{
		{Style: app.theme.style(colorDim), Text: truncateText(pathLine, width)},
		{Style: app.theme.style(colorDim), Text: truncateText(modelText, width)},
	}
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
