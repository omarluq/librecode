package terminal

import (
	"slices"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/transcript"
	"github.com/omarluq/librecode/internal/tui"
)

const transcriptListPageItems = 5

func emptyTranscriptListSelection() transcriptListSelection {
	return transcriptListSelection{
		MessageIndex: 0,
		ItemIndex:    0,
		Active:       false,
	}
}

func (app *App) transcriptListFocused() bool {
	return app.transcriptList.Active
}

func (app *App) blurTranscriptList() {
	app.transcriptList = emptyTranscriptListSelection()
}

func (app *App) focusLatestTranscriptList() bool {
	width := app.currentLineCacheStateWidth()
	app.transcript.LineCache.ensure(app, width, len(app.transcript.History))

	for index, message := range slices.Backward(app.transcript.History) {
		if message.Role != transcript.RoleAssistant {
			continue
		}

		app.transcript.LineCache.lines(app, width, index)

		items := app.transcript.LineCache.items[index].ListItems
		if len(items) == 0 {
			continue
		}

		app.transcriptList = transcriptListSelection{
			MessageIndex: index,
			ItemIndex:    0,
			Active:       true,
		}
		app.ensureSelectedTranscriptListItemVisible(width)

		return true
	}

	return false
}

func (app *App) handleTranscriptListPriorityKey(event *tcell.EventKey) bool {
	return app.handleInlineListKey(
		event,
		app.transcriptListFocused(),
		app.focusLatestTranscriptList,
		app.moveTranscriptListSelection,
		app.blurTranscriptList,
		transcriptListPageItems,
	)
}

func (app *App) handleInlineListKey(
	event *tcell.EventKey,
	focused bool,
	focus func() bool,
	move func(int),
	blur func(),
	pageItems int,
) bool {
	if !focused {
		return app.keys.matches(event, actionInputTab) && focus()
	}

	switch {
	case app.keys.matches(event, actionSelectUp):
		move(-1)
	case app.keys.matches(event, actionSelectDown):
		move(1)
	case app.keys.matches(event, actionSelectPageUp):
		move(-pageItems)
	case app.keys.matches(event, actionSelectPageDown):
		move(pageItems)
	case app.keys.matches(event, actionInputTab), app.keys.matches(event, actionSelectCancel):
		blur()
	default:
		return false
	}

	return true
}

func (app *App) moveTranscriptListSelection(delta int) {
	if !app.validateTranscriptListSelection() {
		return
	}

	items := app.transcript.LineCache.items[app.transcriptList.MessageIndex].ListItems
	app.transcriptList.ItemIndex = min(max(0, app.transcriptList.ItemIndex+delta), len(items)-1)
	app.ensureSelectedTranscriptListItemVisible(app.currentLineCacheStateWidth())
}

func (app *App) validateTranscriptListSelection() bool {
	if !app.transcriptList.Active {
		return false
	}

	messageIndex := app.transcriptList.MessageIndex
	if messageIndex < 0 || messageIndex >= len(app.transcript.History) {
		app.blurTranscriptList()

		return false
	}

	width := app.currentLineCacheStateWidth()
	app.transcript.LineCache.lines(app, width, messageIndex)

	items := app.transcript.LineCache.items[messageIndex].ListItems
	if app.transcriptList.ItemIndex < 0 || app.transcriptList.ItemIndex >= len(items) {
		app.blurTranscriptList()

		return false
	}

	return true
}

func (app *App) ensureSelectedTranscriptListItemVisible(width int) {
	if app.transcript.LastMaxRows <= 0 || !app.validateTranscriptListSelection() {
		return
	}

	app.transcript.LineCache.rebuildPrefixes(app, width)
	messageIndex := app.transcriptList.MessageIndex
	item := app.transcript.LineCache.items[messageIndex].ListItems[app.transcriptList.ItemIndex]
	itemStart := app.transcript.LineCache.prefixes[messageIndex] + item.StartLine
	itemEnd := app.transcript.LineCache.prefixes[messageIndex] + item.EndLine
	staticRows := app.transcript.LineCache.prefixes[len(app.transcript.History)]
	dynamicRows := extraGroupsVisibleRows(app.dynamicMessageLineGroups(width))
	totalRows := staticRows + dynamicRows
	maxRows := app.transcript.LastMaxRows

	if itemEnd-itemStart > maxRows {
		app.scrollOffset = max(0, totalRows-(itemStart+maxRows))

		return
	}

	viewportEnd := totalRows - app.scrollOffset

	viewportStart := max(0, viewportEnd-maxRows)
	if itemStart < viewportStart {
		app.scrollOffset = max(0, totalRows-(itemStart+maxRows))
	} else if itemEnd > viewportEnd {
		app.scrollOffset = max(0, totalRows-itemEnd)
	}
}

func applyLineStyle(line tui.Line, style tcell.Style) tui.Line {
	styled := line.Clone()

	styled.Style = style
	for index := range styled.Spans {
		styled.Spans[index].Style = style
	}

	return styled
}
