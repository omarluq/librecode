package terminal

import (
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
		app.promptHistoryDraft = app.composerText()
	}
	if app.promptHistoryIndex > 0 {
		app.promptHistoryIndex--
	}
	app.setComposerText(app.promptHistory[app.promptHistoryIndex])

	return true
}

func (app *App) showNextPrompt() bool {
	if len(app.promptHistory) == 0 || app.promptHistoryIndex >= len(app.promptHistory) {
		return false
	}
	if app.promptHistoryIndex < len(app.promptHistory)-1 {
		app.promptHistoryIndex++
		app.setComposerText(app.promptHistory[app.promptHistoryIndex])
		return true
	}
	app.promptHistoryIndex = len(app.promptHistory)
	app.setComposerText(app.promptHistoryDraft)
	app.promptHistoryDraft = ""

	return true
}

func (app *App) recordPromptHistory(text string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return
	}
	if len(app.promptHistory) > 0 && app.promptHistory[len(app.promptHistory)-1] == trimmed {
		app.resetPromptHistoryNavigation()
		return
	}
	if len(app.promptHistory) == promptHistoryLimit {
		app.promptHistory = append(app.promptHistory[:0], app.promptHistory[1:]...)
	}
	app.promptHistory = append(app.promptHistory, trimmed)
	app.resetPromptHistoryNavigation()
}

func (app *App) resetPromptHistory() {
	app.promptHistory = []string{}
	app.resetPromptHistoryNavigation()
}

func (app *App) resetPromptHistoryNavigation() {
	app.promptHistoryIndex = len(app.promptHistory)
	app.promptHistoryDraft = ""
}
