package terminal

import (
	"github.com/omarluq/librecode/internal/transcript"
	"github.com/omarluq/librecode/internal/tui"
)

func (app *App) allMessageLines(width int, dynamicGroups [][]tui.Line) []tui.Line {
	groups := make([][]tui.Line, 0, len(app.transcript.History)+len(dynamicGroups))
	for index := range app.transcript.History {
		groups = append(groups, app.cachedMessageLines(width, index))
	}

	groups = append(groups, dynamicGroups...)

	return flattenStyledLineGroups(groups, styledLineGroupRows(groups))
}

func (app *App) bottomMessageLines(width, maxRows int, dynamicGroups [][]tui.Line) []tui.Line {
	reservedRows := extraGroupsVisibleRows(dynamicGroups)
	staticMaxRows := max(0, maxRows-reservedRows)

	groups := make([][]tui.Line, 0, len(app.transcript.History)+len(dynamicGroups))
	if staticMaxRows > 0 {
		staticGroups, _ := app.tailStaticMessageGroups(width, staticMaxRows)
		groups = append(groups, staticGroups...)
	}

	groups = append(groups, dynamicGroups...)

	return sliceBottomStyledLineGroups(groups, maxRows)
}

func (app *App) scrolledMessageLines(width, maxRows int, dynamicGroups [][]tui.Line) []tui.Line {
	if maxRows <= 0 {
		return nil
	}

	app.transcript.LineCache.ensure(app, width, len(app.transcript.History))

	if !app.transcript.LineCache.warm {
		return app.scrolledMessageLinesFromTail(width, maxRows, dynamicGroups)
	}

	staticRows := app.transcript.LineCache.prefixes[len(app.transcript.History)]
	dynamicRows := extraGroupsVisibleRows(dynamicGroups)

	totalRows := staticRows + dynamicRows
	if totalRows <= maxRows {
		app.scrollOffset = 0

		return app.allMessageLines(width, dynamicGroups)
	}

	app.scrollOffset = min(app.scrollOffset, totalRows-maxRows)
	endRow := totalRows - app.scrollOffset
	startRow := max(0, endRow-maxRows)

	lines := make([]tui.Line, 0, endRow-startRow)
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

func (app *App) scrolledMessageLinesFromTail(width, maxRows int, dynamicGroups [][]tui.Line) []tui.Line {
	dynamicRows := extraGroupsVisibleRows(dynamicGroups)
	rowsNeededFromBottom := maxRows + app.scrollOffset
	staticRowsNeeded := max(0, rowsNeededFromBottom-dynamicRows)
	staticGroups, reachedStart := app.tailStaticMessageGroups(width, staticRowsNeeded)
	groups := make([][]tui.Line, 0, len(staticGroups)+len(dynamicGroups))
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

func (app *App) tailStaticMessageGroups(width, rowsNeeded int) ([][]tui.Line, bool) {
	if rowsNeeded <= 0 || len(app.transcript.History) == 0 {
		return nil, len(app.transcript.History) == 0
	}

	rows := 0
	start := len(app.transcript.History)

	var partial []tui.Line

	for start > 0 && rows < rowsNeeded {
		start--
		remaining := rowsNeeded - rows

		lines, complete := app.cachedMessageTailLines(width, start, remaining)
		if !complete {
			partial = lines

			break
		}

		rows += len(lines)
	}

	groups := make([][]tui.Line, 0, len(app.transcript.History)-start)
	if partial != nil {
		groups = append(groups, partial)
		start++
	}

	for index := start; index < len(app.transcript.History); index++ {
		groups = append(groups, app.cachedMessageLines(width, index))
	}

	return groups, start == 0 && partial == nil
}

func (app *App) cachedMessageTailLines(width, index, rowsNeeded int) ([]tui.Line, bool) {
	if rowsNeeded <= 0 {
		return nil, true
	}

	if app.toolsExpanded && app.transcript.History[index].Role == transcript.RoleToolResult {
		return app.renderToolMessageTail(width, app.transcript.History[index], rowsNeeded)
	}

	lines := app.cachedMessageLines(width, index)

	return lines, true
}

func (app *App) staticMessageLinesForRows(width, startRow, endRow int) []tui.Line {
	if endRow <= startRow || len(app.transcript.History) == 0 {
		return nil
	}

	app.rebuildMessageRowPrefixSums(width)
	app.transcript.LineCache.warm = true
	startIndex := lowerBoundInts(app.transcript.LineCache.prefixes, startRow+1) - 1
	endIndex := lowerBoundInts(app.transcript.LineCache.prefixes, endRow)
	startIndex = min(max(0, startIndex), len(app.transcript.History))
	endIndex = min(max(startIndex, endIndex), len(app.transcript.History))

	groups := make([][]tui.Line, 0, endIndex-startIndex)
	for index := startIndex; index < endIndex; index++ {
		groups = append(groups, app.cachedMessageLines(width, index))
	}

	relativeStart := startRow - app.transcript.LineCache.prefixes[startIndex]
	relativeEnd := endRow - app.transcript.LineCache.prefixes[startIndex]

	return sliceStyledLineGroups(groups, relativeStart, relativeEnd)
}

func (app *App) dynamicMessageLineGroups(width int) [][]tui.Line {
	groups := make([][]tui.Line, 0, len(app.transcript.Streaming.Blocks)+messageMetadataRows)
	if len(app.transcript.Streaming.Blocks) > 0 {
		for index := range app.transcript.Streaming.Blocks {
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

	if app.busy() {
		groups = append(groups, app.renderWorkingIndicator(width))
	}

	if len(app.queuedMessages) > 0 {
		groups = append(groups, app.renderQueuedMessages(width))
	}

	return groups
}

func (app *App) currentLineCacheStateWidth() int {
	state := app.transcript.LineCache.state
	if state.Width > 0 {
		return state.Width
	}

	width, _ := app.screenSize()

	return width
}

func lowerBoundInts(values []int, target int) int {
	low, high := 0, len(values)
	for low < high {
		mid := low + (high-low)/terminalHalf
		if values[mid] < target {
			low = mid + 1
		} else {
			high = mid
		}
	}

	return low
}

func extraGroupsVisibleRows(groups [][]tui.Line) int {
	return styledLineGroupRows(groups)
}

func styledLineGroupRows(groups [][]tui.Line) int {
	total := 0
	for _, group := range groups {
		total += len(group)
	}

	return total
}

func sliceBottomStyledLineGroups(groups [][]tui.Line, maxRows int) []tui.Line {
	totalRows := styledLineGroupRows(groups)
	if maxRows < 0 || totalRows <= maxRows {
		return flattenStyledLineGroups(groups, totalRows)
	}

	return sliceStyledLineGroups(groups, totalRows-maxRows, totalRows)
}

func (app *App) visibleMessageLineGroups(groups [][]tui.Line, maxRows int) []tui.Line {
	totalRows := 0
	for _, group := range groups {
		totalRows += len(group)
	}

	if maxRows < 0 || totalRows <= maxRows {
		return flattenStyledLineGroups(groups, totalRows)
	}

	maxOffset := max(0, totalRows-maxRows)
	offset := min(app.scrollOffset, maxOffset)
	end := totalRows - offset
	start := max(0, end-maxRows)

	return sliceStyledLineGroups(groups, start, end)
}

func flattenStyledLineGroups(groups [][]tui.Line, totalRows int) []tui.Line {
	lines := make([]tui.Line, 0, totalRows)
	for _, group := range groups {
		lines = append(lines, group...)
	}

	return lines
}

func sliceStyledLineGroups(groups [][]tui.Line, start, end int) []tui.Line {
	lines := make([]tui.Line, 0, max(0, end-start))

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
