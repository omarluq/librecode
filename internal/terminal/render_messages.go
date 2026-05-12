package terminal

import (
	"strings"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/database"
)

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
	if !app.messageCacheWarm {
		return app.scrolledMessageLinesFromTail(width, maxRows, dynamicGroups)
	}

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

func (app *App) scrolledMessageLinesFromTail(width, maxRows int, dynamicGroups [][]styledLine) []styledLine {
	dynamicRows := extraGroupsVisibleRows(dynamicGroups)
	rowsNeededFromBottom := maxRows + app.scrollOffset
	staticRowsNeeded := max(0, rowsNeededFromBottom-dynamicRows)
	staticGroups, reachedStart := app.tailStaticMessageGroups(width, staticRowsNeeded)
	groups := make([][]styledLine, 0, len(staticGroups)+len(dynamicGroups))
	groups = append(groups, staticGroups...)
	groups = append(groups, dynamicGroups...)
	totalRows := styledLineGroupRows(groups)
	if reachedStart && totalRows <= maxRows {
		app.scrollOffset = 0

		return flattenStyledLineGroups(groups, totalRows)
	}
	if reachedStart {
		app.scrollOffset = min(app.scrollOffset, max(0, totalRows-maxRows))
	}
	endRow := max(0, totalRows-app.scrollOffset)
	startRow := max(0, endRow-maxRows)

	return sliceStyledLineGroups(groups, startRow, endRow)
}

func (app *App) tailStaticMessageGroups(width, rowsNeeded int) ([][]styledLine, bool) {
	if rowsNeeded <= 0 || len(app.messages) == 0 {
		return nil, len(app.messages) == 0
	}
	rows := 0
	start := len(app.messages)
	for start > 0 && rows < rowsNeeded {
		start--
		rows += len(app.cachedMessageLines(width, start))
	}
	groups := make([][]styledLine, 0, len(app.messages)-start)
	for index := start; index < len(app.messages); index++ {
		groups = append(groups, app.cachedMessageLines(width, index))
	}

	return groups, start == 0
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

const messageCacheWarmBatchSize = 16

func (app *App) warmMessageLineCache() {
	for !app.messageCacheWarm {
		app.warmMessageLineCacheStep()
	}
}

func (app *App) warmMessageLineCacheStep() {
	if app.messageCacheWarm || len(app.messages) == 0 || app.lastMessageMaxRows <= 0 {
		return
	}
	width := app.currentLineCacheStateWidth()
	app.ensureMessageLineCache(width)
	start := min(max(0, app.messageCacheWarmIndex), len(app.messages))
	end := min(len(app.messages), start+messageCacheWarmBatchSize)
	for index := start; index < end; index++ {
		if !app.messageLineCache[index].Valid {
			app.messageLineCache[index] = cachedRenderedMessage{
				Lines: app.renderMessage(width, app.messages[index]),
				Valid: true,
			}
		}
	}
	app.messageCacheWarmIndex = end
	if end < len(app.messages) {
		return
	}
	app.rebuildMessageRowPrefixSumsFromCache()
	app.messageCacheWarm = true
}

func (app *App) rebuildMessageRowPrefixSumsFromCache() {
	prefixSums := make([]int, len(app.messageLineCache)+1)
	for index := range app.messageLineCache {
		prefixSums[index+1] = prefixSums[index] + len(app.messageLineCache[index].Lines)
	}
	app.messageRowPrefixSums = prefixSums
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
		newStyledLine(app.theme.style(colorDim), ""),
		newStyledLine(app.theme.background(colorUserMessageBg), padRight("", width)),
	)
	for _, line := range wrapped {
		text := "  " + padRight(line, innerWidth) + "  "
		lines = append(lines, newStyledLine(app.theme.background(colorUserMessageBg), text))
	}
	lines = append(lines,
		newStyledLine(app.theme.background(colorUserMessageBg), padRight("", width)),
		newStyledLine(app.theme.style(colorDim), ""),
	)

	return lines
}

func (app *App) renderQueuedMessages(width int) []styledLine {
	style := app.theme.background(colorUserMessageBg).Foreground(app.theme.colors[colorMuted])
	innerWidth := max(1, width-4)
	lines := []styledLine{newStyledLine(app.theme.style(colorDim), "")}
	for index, message := range app.queuedMessages {
		header := "queued follow-up " + intText(index+1)
		lines = append(lines, newStyledLine(style.Bold(true), "  "+padRight(header, innerWidth)+"  "))
		for _, line := range wrapText(message, innerWidth) {
			lines = append(lines, newStyledLine(style, "  "+padRight(line, innerWidth)+"  "))
		}
	}
	lines = append(lines, newStyledLine(app.theme.style(colorDim), ""))

	return lines
}

func (app *App) renderAssistantMessage(width int, content string) []styledLine {
	markdownLines := app.renderMarkdown(strings.TrimSpace(content), width)
	lines := make([]styledLine, 0, len(markdownLines)+2)
	lines = append(lines, newStyledLine(app.theme.style(colorDim), ""))
	lines = append(lines, markdownLines...)
	lines = append(lines, newStyledLine(app.theme.style(colorDim), ""))

	return lines
}

func (app *App) renderStreamingMessage(width int, content string) []styledLine {
	wrapped := wrapText(strings.TrimSpace(content), width)
	style := app.theme.style(colorText)
	lines := make([]styledLine, 0, len(wrapped)+2)
	lines = append(lines, newStyledLine(app.theme.style(colorDim), ""))
	for _, line := range wrapped {
		lines = append(lines, newStyledLine(style, line))
	}
	lines = append(lines, newStyledLine(app.theme.style(colorDim), ""))

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
			newStyledLine(tcell.StyleDefault, ""),
			newStyledLine(style, "thinking…"),
			newStyledLine(tcell.StyleDefault, ""),
		}
	}

	markdownLines := app.renderMarkdown(strings.TrimSpace(message.Content), width)
	lines := make([]styledLine, 0, len(markdownLines)+3)
	lines = append(lines,
		newStyledLine(tcell.StyleDefault, ""),
		newStyledLine(style.Bold(true), settingThinking),
	)
	for _, line := range markdownLines {
		lines = append(lines, newStyledLine(style, line.Text))
	}
	lines = append(lines, newStyledLine(app.theme.style(colorDim), ""))

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
		newStyledLine(tcell.StyleDefault, ""),
		newStyledLine(style, padRight("", width)),
		newStyledLine(style.Bold(true), padRight("  ["+label+"]", width)),
	)
	for _, line := range wrapped {
		lines = append(lines, newStyledLine(style, "  "+padRight(line, innerWidth)+"  "))
	}
	lines = append(lines,
		newStyledLine(style, padRight("", width)),
		newStyledLine(tcell.StyleDefault, ""),
	)

	return lines
}
