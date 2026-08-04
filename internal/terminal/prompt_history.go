package terminal

import (
	"slices"
	"strings"

	"github.com/gdamore/tcell/v3"
)

const promptHistoryLimit = 100

func (app *App) handlePromptHistoryKey(event *tcell.EventKey) bool {
	if app.autocompleteActive() {
		return false
	}

	if app.keys.matches(event, actionCursorUp) {
		return app.showPreviousPrompt()
	}

	if app.keys.matches(event, actionCursorDown) {
		return app.showNextPrompt()
	}

	return false
}

func (app *App) showPreviousPrompt() bool {
	if len(app.promptHistory) == 0 {
		return false
	}

	if app.promptHistoryIndex == len(app.promptHistory) {
		app.promptHistoryDraft = app.composerBuffer.TextValue()
		app.promptHistoryDraftImages = cloneImageAttachments(app.composerImages)
	}

	if app.promptHistoryIndex > 0 {
		app.promptHistoryIndex--
	}

	app.restorePromptHistoryIndex(app.promptHistoryIndex)

	return true
}

func (app *App) showNextPrompt() bool {
	if len(app.promptHistory) == 0 || app.promptHistoryIndex >= len(app.promptHistory) {
		return false
	}

	if app.promptHistoryIndex < len(app.promptHistory)-1 {
		app.promptHistoryIndex++
		app.restorePromptHistoryIndex(app.promptHistoryIndex)

		return true
	}

	app.promptHistoryIndex = len(app.promptHistory)
	app.composerBuffer.SetText(app.promptHistoryDraft)
	app.composerImages = cloneImageAttachments(app.promptHistoryDraftImages)
	app.promptHistoryDraft = ""
	app.promptHistoryDraftImages = nil

	return true
}

func (app *App) restorePromptHistoryIndex(index int) {
	app.composerBuffer.SetText(app.promptHistory[index])

	app.composerImages = nil
	if index < len(app.promptHistoryImages) {
		app.composerImages = cloneImageAttachments(app.promptHistoryImages[index])
	}
}

func (app *App) recordPromptHistory(text string) {
	app.recordPromptDraftHistory(promptDraft{Text: text, Images: nil})
}

func (app *App) recordPromptDraftHistory(draft promptDraft) {
	trimmed := strings.TrimSpace(draft.Text)
	if trimmed == "" && len(draft.Images) == 0 {
		return
	}

	last := len(app.promptHistory) - 1

	var lastImages []imageAttachment
	if last >= 0 && last < len(app.promptHistoryImages) {
		lastImages = app.promptHistoryImages[last]
	}

	if last >= 0 && app.promptHistory[last] == trimmed && attachmentSummariesEqual(lastImages, draft.Images) {
		app.resetPromptHistoryNavigation()

		return
	}

	if len(app.promptHistory) == promptHistoryLimit {
		app.promptHistory = append(app.promptHistory[:0], app.promptHistory[1:]...)
		app.promptHistoryImages = append(app.promptHistoryImages[:0], app.promptHistoryImages[1:]...)
	}

	app.promptHistory = append(app.promptHistory, trimmed)
	app.promptHistoryImages = append(app.promptHistoryImages, cloneImageAttachments(draft.Images))
	app.resetPromptHistoryNavigation()
}

func (app *App) resetPromptHistory() {
	app.promptHistory = []string{}
	app.promptHistoryImages = [][]imageAttachment{}
	app.resetPromptHistoryNavigation()
}

func (app *App) resetPromptHistoryNavigation() {
	app.promptHistoryIndex = len(app.promptHistory)
	app.promptHistoryDraft = ""
	app.promptHistoryDraftImages = nil
}

func attachmentSummariesEqual(left, right []imageAttachment) bool {
	if len(left) != len(right) {
		return false
	}

	for index := range left {
		if left[index].Name != right[index].Name || left[index].MIMEType != right[index].MIMEType ||
			left[index].Width != right[index].Width || left[index].Height != right[index].Height ||
			!slices.Equal(left[index].Data, right[index].Data) {
			return false
		}
	}

	return true
}
