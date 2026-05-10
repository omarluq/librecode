package terminal

import (
	"context"
	"strings"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/extension"
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

	return 80, 24
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
	app.frame = newCellBuffer(width, height, tcell.StyleDefault)
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
	if app.hasExtensionHandlers(extensionEventRender) || app.runtimeLayout != nil || len(app.runtimeWindows) > 0 {
		return true
	}
	if len(app.uiWindowOverrides) > 0 || app.uiCursor != nil {
		return true
	}
	_, transcriptOverridden := app.extensionRuntimeBuffers[extensionBufferTranscript]

	return transcriptOverridden
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
	app.lastMessageMaxRows = availableRows
	lines := app.messageLines(width, availableRows)
	for _, line := range lines {
		app.writeStyledLine(row, width, line)
		row++
	}

	return row
}

func (app *App) drawTranscriptWindow(layout *runtimeLayout) {
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

func (app *App) drawPanelWindow(layout *runtimeLayout) {
	window := layout.Transcript
	if !window.Visible || window.Height <= 0 || app.extensionOwnsWindow(window.Name) {
		return
	}
	lines := app.panel.render(window.Width, window.Height, app.theme, app.keys)
	for index, line := range lines {
		app.writeStyledLine(window.Y+index, window.Width, line)
	}
}

func (app *App) messageLines(width, maxRows int) []styledLine {
	app.lastMessageMaxRows = maxRows
	dynamicGroups := app.dynamicMessageLineGroups(width)
	if maxRows < 0 {
		return app.allMessageLines(width, dynamicGroups)
	}
	if app.scrollOffset == 0 {
		return app.bottomMessageLines(width, maxRows, dynamicGroups)
	}

	return app.scrolledMessageLines(width, maxRows, dynamicGroups)
}

func (app *App) allMessageLines(width int, dynamicGroups [][]styledLine) []styledLine {
	groups := make([][]styledLine, 0, len(app.messages)+len(dynamicGroups))
	for index := range app.messages {
		groups = append(groups, app.cachedMessageLines(width, index))
	}
	groups = append(groups, dynamicGroups...)

	return flattenStyledLineGroups(groups, styledLineGroupRows(groups))
}

func (app *App) bottomMessageLines(width, maxRows int, dynamicGroups [][]styledLine) []styledLine {
	reservedRows := extraGroupsVisibleRows(dynamicGroups)
	staticMaxRows := max(0, maxRows-reservedRows)
	groups := make([][]styledLine, 0, len(app.messages)+len(dynamicGroups))
	if staticMaxRows > 0 {
		start := app.tailStaticMessageRange(width, staticMaxRows)
		for index := start; index < len(app.messages); index++ {
			groups = append(groups, app.cachedMessageLines(width, index))
		}
	}
	groups = append(groups, dynamicGroups...)

	return sliceBottomStyledLineGroups(groups, maxRows)
}

func (app *App) scrolledMessageLines(width, maxRows int, dynamicGroups [][]styledLine) []styledLine {
	if maxRows <= 0 {
		return nil
	}
	app.rebuildMessageRowPrefixSums(width)
	app.messageCacheWarm = true
	staticRows := app.messageRowPrefixSums[len(app.messages)]
	dynamicRows := extraGroupsVisibleRows(dynamicGroups)
	totalRows := staticRows + dynamicRows
	if totalRows <= maxRows {
		app.scrollOffset = 0

		return app.allMessageLines(width, dynamicGroups)
	}
	app.scrollOffset = min(app.scrollOffset, totalRows-maxRows)
	endRow := totalRows - app.scrollOffset
	startRow := max(0, endRow-maxRows)
	lines := make([]styledLine, 0, endRow-startRow)
	if startRow < staticRows {
		lines = append(lines, app.staticMessageLinesForRows(width, startRow, min(endRow, staticRows))...)
	}
	if endRow > staticRows {
		dynamicStart := max(0, startRow-staticRows)
		dynamicEnd := min(dynamicRows, endRow-staticRows)
		lines = append(lines, sliceStyledLineGroups(dynamicGroups, dynamicStart, dynamicEnd)...)
	}

	return lines
}

func (app *App) staticMessageLinesForRows(width, startRow, endRow int) []styledLine {
	if endRow <= startRow || len(app.messages) == 0 {
		return nil
	}
	app.rebuildMessageRowPrefixSums(width)
	app.messageCacheWarm = true
	startIndex := lowerBoundInts(app.messageRowPrefixSums, startRow+1) - 1
	endIndex := lowerBoundInts(app.messageRowPrefixSums, endRow)
	startIndex = min(max(0, startIndex), len(app.messages))
	endIndex = min(max(startIndex, endIndex), len(app.messages))
	groups := make([][]styledLine, 0, endIndex-startIndex)
	for index := startIndex; index < endIndex; index++ {
		groups = append(groups, app.cachedMessageLines(width, index))
	}
	relativeStart := startRow - app.messageRowPrefixSums[startIndex]
	relativeEnd := endRow - app.messageRowPrefixSums[startIndex]

	return sliceStyledLineGroups(groups, relativeStart, relativeEnd)
}

func (app *App) dynamicMessageLineGroups(width int) [][]styledLine {
	groups := make([][]styledLine, 0, len(app.streamingBlocks)+3)
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
		groups = append(groups, app.renderWorkingIndicator(width))
	}
	if len(app.queuedMessages) > 0 {
		groups = append(groups, app.renderQueuedMessages(width))
	}

	return groups
}

func (app *App) renderWorkingIndicator(_ int) []styledLine {
	return []styledLine{
		{Style: tcell.StyleDefault, Text: ""},
		{Style: app.workingIndicatorStyle(), Text: "  " + app.workingIndicator()},
		{Style: tcell.StyleDefault, Text: ""},
	}
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
	app.messageRowPrefixSums = nil

	return lines
}

func (app *App) ensureMessageLineCache(width int) {
	app.ensureLineCache(width, len(app.messages), &app.messageLineCache, &app.messageLineCacheState)
	if len(app.messageRowPrefixSums) != len(app.messageLineCache)+1 {
		app.messageRowPrefixSums = nil
	}
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
		app.messageRowPrefixSums = nil
		app.messageCacheWarm = false
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

func (app *App) tailStaticMessageRange(width, maxRows int) int {
	remainingRows := maxRows
	for index := len(app.messages) - 1; index >= 0; index-- {
		remainingRows -= len(app.cachedMessageLines(width, index))
		if remainingRows <= 0 {
			return index
		}
	}

	return 0
}

func (app *App) rebuildMessageRowPrefixSums(width int) {
	app.ensureMessageLineCache(width)
	prefixSums := make([]int, len(app.messageLineCache)+1)
	for index := range app.messageLineCache {
		if !app.messageLineCache[index].Valid {
			app.messageLineCache[index] = cachedRenderedMessage{
				Lines: app.renderMessage(width, app.messages[index]),
				Valid: true,
			}
		}
		prefixSums[index+1] = prefixSums[index] + len(app.messageLineCache[index].Lines)
	}
	app.messageRowPrefixSums = prefixSums
}

func (app *App) warmMessageLineCache() {
	if app.messageCacheWarm || len(app.messages) == 0 || app.lastMessageMaxRows <= 0 {
		return
	}
	app.rebuildMessageRowPrefixSums(app.currentLineCacheStateWidth())
	app.messageCacheWarm = true
}

func (app *App) currentLineCacheStateWidth() int {
	state := app.messageLineCacheState
	if state.Width > 0 {
		return state.Width
	}
	width, _ := app.screenSize()

	return width
}

func lowerBoundInts(values []int, target int) int {
	low, high := 0, len(values)
	for low < high {
		mid := low + (high-low)/2
		if values[mid] < target {
			low = mid + 1
		} else {
			high = mid
		}
	}

	return low
}

func extraGroupsVisibleRows(groups [][]styledLine) int {
	return styledLineGroupRows(groups)
}

func styledLineGroupRows(groups [][]styledLine) int {
	total := 0
	for _, group := range groups {
		total += len(group)
	}

	return total
}

func sliceBottomStyledLineGroups(groups [][]styledLine, maxRows int) []styledLine {
	totalRows := styledLineGroupRows(groups)
	if maxRows < 0 || totalRows <= maxRows {
		return flattenStyledLineGroups(groups, totalRows)
	}

	return sliceStyledLineGroups(groups, totalRows-maxRows, totalRows)
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

func (app *App) drawAutocompleteWindow(layout *runtimeLayout) {
	window := layout.Autocomplete
	if !window.Visible || window.Height <= 0 || app.extensionOwnsWindow(window.Name) {
		return
	}
	lines := app.autocompleteLines(window.Width)
	for index, line := range lines {
		writeLine(app.frame, window.Y+index, window.Width, line.Text, line.Style)
	}
}

func (app *App) drawComposerWindow(layout *runtimeLayout) {
	window := layout.Composer
	if !window.Visible || window.Height <= 0 || app.extensionOwnsWindow(window.Name) {
		return
	}
	editor := app.renderComposerEditor(window.Width, max(1, window.Height-2))
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

func (app *App) renderComposerEditor(width, bodyRows int) editorRender {
	return renderEditor(
		[]rune(app.composerText()),
		app.composerCursor(),
		width,
		bodyRows,
		app.theme,
		app.editorBorderColor(),
		app.composerBorderLabel(),
	)
}

func (app *App) drawStatusWindow(layout *runtimeLayout) {
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
	maxEditorRows := min(defaultEditorRows, max(3, height-len(footerLines)-len(autocompleteLines)-2))
	maxEditorRows = max(3, maxEditorRows)
	editor := app.renderComposerEditor(width, maxEditorRows-2)
	reserve := len(footerLines) + len(autocompleteLines) + len(editor.Lines)
	if reserve > height {
		bodyRows := max(1, height-len(footerLines)-len(autocompleteLines)-2)
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

func (app *App) currentRuntimeLayout() runtimeLayout {
	width, height := app.screenSize()

	return app.currentRuntimeLayoutForSize(width, height)
}

func (app *App) currentRuntimeLayoutForSize(width, height int) runtimeLayout {
	layout := app.defaultRuntimeLayout(width, height)

	return app.mergeRuntimeLayout(&layout)
}

func (app *App) defaultRuntimeLayout(width, height int) runtimeLayout {
	statusLines := app.footerLines(width)
	autocompleteLines := app.autocompleteLines(width)
	maxComposerHeight := min(defaultEditorRows, max(3, height-len(statusLines)-len(autocompleteLines)-2))
	maxComposerHeight = max(3, maxComposerHeight)
	composerHeight := len(app.renderComposerEditor(width, max(1, maxComposerHeight-2)).Lines)
	reservedRows := len(statusLines) + len(autocompleteLines) + composerHeight
	if reservedRows > height {
		composerHeight = max(3, height-len(statusLines)-len(autocompleteLines))
		reservedRows = len(statusLines) + len(autocompleteLines) + composerHeight
	}
	transcriptHeight := max(0, height-reservedRows)
	autocompleteStart := transcriptHeight
	composerStart := autocompleteStart + len(autocompleteLines)
	statusStart := height - len(statusLines)

	return runtimeLayout{
		Windows: nil,
		Width:   width,
		Height:  height,
		Transcript: extension.WindowState{
			Metadata:  map[string]any{extensionMetadataCount: len(app.messages)},
			Name:      extensionBufferTranscript,
			Role:      extensionBufferTranscript,
			Buffer:    extensionBufferTranscript,
			Renderer:  windowRendererDefault,
			X:         0,
			Y:         0,
			Width:     width,
			Height:    transcriptHeight,
			CursorRow: 0,
			CursorCol: 0,
			Visible:   true,
		},
		Autocomplete: extension.WindowState{
			Metadata:  map[string]any{},
			Name:      "autocomplete",
			Role:      "autocomplete",
			Buffer:    extensionBufferStatus,
			Renderer:  windowRendererDefault,
			X:         0,
			Y:         autocompleteStart,
			Width:     width,
			Height:    len(autocompleteLines),
			CursorRow: 0,
			CursorCol: 0,
			Visible:   len(autocompleteLines) > 0,
		},
		Composer: extension.WindowState{
			Metadata:  map[string]any{},
			Name:      extensionBufferComposer,
			Role:      extensionBufferComposer,
			Buffer:    extensionBufferComposer,
			Renderer:  windowRendererDefault,
			X:         0,
			Y:         composerStart,
			Width:     width,
			Height:    composerHeight,
			CursorRow: 0,
			CursorCol: 0,
			Visible:   true,
		},
		Status: extension.WindowState{
			Metadata:  map[string]any{},
			Name:      extensionBufferStatus,
			Role:      extensionBufferStatus,
			Buffer:    extensionBufferStatus,
			Renderer:  windowRendererDefault,
			X:         0,
			Y:         statusStart,
			Width:     width,
			Height:    len(statusLines),
			CursorRow: 0,
			CursorCol: 0,
			Visible:   true,
		},
	}
}

func (app *App) mergeRuntimeLayout(layout *runtimeLayout) runtimeLayout {
	windows := app.cloneRuntimeWindows(layout)
	transcript := windows[extensionBufferTranscript]
	autocomplete := windows["autocomplete"]
	composer := windows[extensionBufferComposer]
	status := windows[extensionBufferStatus]

	return runtimeLayout{
		Width:        layout.Width,
		Height:       layout.Height,
		Transcript:   transcript,
		Autocomplete: autocomplete,
		Composer:     composer,
		Status:       status,
		Windows:      windows,
	}
}

func (app *App) extensionOwnsWindow(name string) bool {
	window, ok := app.runtimeWindows[name]
	if !ok {
		return false
	}

	return strings.EqualFold(strings.TrimSpace(window.Renderer), windowRendererExtension)
}

func (app *App) editorBorderColor() colorToken {
	if strings.HasPrefix(strings.TrimSpace(app.composerText()), "!") {
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
	lineTexts := app.defaultStatusLineTexts()
	lines := make([]styledLine, 0, len(lineTexts))
	for _, lineText := range lineTexts {
		lines = append(lines, styledLine{Style: app.theme.style(colorDim), Text: truncateText(lineText, width)})
	}

	return lines
}

func (app *App) writeStyledLine(row, width int, line styledLine) {
	if isWorkingIndicatorText(line.Text) {
		writeShimmerLineWithVerticalBorders(
			app.frame,
			row,
			width,
			line.Text,
			line.Style,
			app.theme.colors[colorBorderMuted],
			app.workingShimmerFrame(),
		)
		return
	}

	writeLineWithVerticalBorders(app.frame, row, width, line.Text, line.Style, app.theme.colors[colorBorderMuted])
}

func writeLine(screen cellTarget, row, width int, text string, style tcell.Style) {
	writeTextAt(screen, 0, row, width, text, style)
}

func writeLineWithVerticalBorders(
	screen cellTarget,
	row int,
	width int,
	text string,
	style tcell.Style,
	borderColor tcell.Color,
) {
	writeLineWithStyleFunc(screen, row, width, text, style, func(segment terminalTextSegment, used int) tcell.Style {
		if segment.Text == "│" && (used == 0 || used == width-1) {
			return style.Foreground(borderColor)
		}

		return style
	})
}

func writeTextAt(screen cellTarget, column, row, width int, text string, style tcell.Style) {
	writeTextCells(screen, column, row, width, text, style)
}

func writeShimmerLineWithVerticalBorders(
	screen cellTarget,
	row int,
	width int,
	text string,
	style tcell.Style,
	borderColor tcell.Color,
	frame int,
) {
	spinnerStart, spinnerWidth := workingSpinnerRange(text)
	contentStart, contentWidth := workingShimmerContentRange(text)
	writeLineWithStyleFunc(screen, row, width, text, style, func(segment terminalTextSegment, used int) tcell.Style {
		if segment.Text == "│" && (used == 0 || used == width-1) {
			return style.Foreground(borderColor)
		}
		if spinnerWidth > 0 && used >= spinnerStart && used < spinnerStart+spinnerWidth {
			return style.Foreground(workingShimmerBrightColor())
		}
		if contentWidth == 0 || used < contentStart || used >= contentStart+contentWidth {
			return style
		}

		return style.Foreground(workingShimmerColor(frame, used-contentStart, contentWidth))
	})
}

func isWorkingIndicatorText(text string) bool {
	return workingIndicatorParts(text).label != ""
}

func workingSpinnerRange(text string) (start, width int) {
	parts := workingIndicatorParts(text)
	if parts.label == "" {
		return 0, 0
	}

	return parts.spinnerStart, terminalTextWidth(parts.spinner)
}

func workingShimmerContentRange(text string) (start, width int) {
	parts := workingIndicatorParts(text)
	if parts.label == "" {
		return 0, 0
	}

	return parts.labelStart, terminalTextWidth(parts.label)
}

type workingIndicatorPartsResult struct {
	spinner      string
	label        string
	spinnerStart int
	labelStart   int
}

func emptyWorkingIndicatorParts() workingIndicatorPartsResult {
	return workingIndicatorPartsResult{
		spinner:      "",
		label:        "",
		spinnerStart: 0,
		labelStart:   0,
	}
}

func workingIndicatorParts(text string) workingIndicatorPartsResult {
	trimmedLeft := strings.TrimLeft(text, " ")
	spinner := firstField(trimmedLeft)
	if !isWorkingSpinner(spinner) {
		return emptyWorkingIndicatorParts()
	}
	afterSpinner := strings.TrimPrefix(trimmedLeft, spinner)
	label := strings.TrimLeft(strings.TrimRight(afterSpinner, " "), " ")
	if label == "" {
		return emptyWorkingIndicatorParts()
	}
	beforeLabel := text[:len(text)-len(afterSpinner)]
	labelPadding := terminalTextWidth(afterSpinner) - terminalTextWidth(strings.TrimLeft(afterSpinner, " "))

	return workingIndicatorPartsResult{
		spinner:      spinner,
		label:        label,
		spinnerStart: terminalTextWidth(text) - terminalTextWidth(trimmedLeft),
		labelStart:   terminalTextWidth(beforeLabel) + labelPadding,
	}
}

func firstField(text string) string {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return ""
	}

	return fields[0]
}

func isWorkingSpinner(text string) bool {
	switch text {
	case "⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏":
		return true
	default:
		return false
	}
}

func writeLineWithStyleFunc(
	screen cellTarget,
	row int,
	width int,
	text string,
	defaultStyle tcell.Style,
	styleFor func(segment terminalTextSegment, used int) tcell.Style,
) {
	if row < 0 || width <= 0 {
		return
	}
	used := 0
	for _, segment := range terminalTextSegments(text) {
		if used+segment.Width > width {
			break
		}
		used += writeTextSegment(screen, used, row, width-used, segment, styleFor(segment, used))
	}
	for used < width {
		screen.SetContent(used, row, ' ', nil, defaultStyle)
		used++
	}
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
	used := writeEditorLineText(screen, row, width, line, borderStyle)
	writeEditorLinePadding(screen, row, width, used, line, borderStyle)
}

func writeEditorLineText(
	screen cellTarget,
	row int,
	width int,
	line styledLine,
	borderStyle tcell.Style,
) int {
	used := 0
	for _, segment := range terminalTextSegments(line.Text) {
		if used+segment.Width > width {
			break
		}
		used += writeTextSegment(
			screen,
			used,
			row,
			width-used,
			segment,
			editorLineStyle(used, width, line, borderStyle),
		)
	}

	return used
}

func writeEditorLinePadding(
	screen cellTarget,
	row int,
	width int,
	used int,
	line styledLine,
	borderStyle tcell.Style,
) {
	for used < width {
		screen.SetContent(used, row, ' ', nil, editorLineStyle(used, width, line, borderStyle))
		used++
	}
}

func editorLineStyle(position, width int, line styledLine, borderStyle tcell.Style) tcell.Style {
	if position < 2 || position >= max(0, width-2) {
		return borderStyle
	}

	return line.Style
}

func (app *App) cloneRuntimeWindows(layout *runtimeLayout) map[string]extension.WindowState {
	windows := map[string]extension.WindowState{
		layout.Transcript.Name:   layout.Transcript,
		layout.Autocomplete.Name: layout.Autocomplete,
		layout.Composer.Name:     layout.Composer,
		layout.Status.Name:       layout.Status,
	}
	if app.runtimeLayout != nil && len(app.runtimeLayout.Windows) > 0 {
		for name := range app.runtimeLayout.Windows {
			windows[name] = app.runtimeLayout.Windows[name]
		}
	}
	for name := range app.runtimeWindows {
		windows[name] = app.runtimeWindows[name]
	}
	for name := range windows {
		window := windows[name]
		if window.Name == "" {
			window.Name = name
		}
		windows[name] = window
	}

	return windows
}

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
