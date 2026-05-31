package terminal

import "github.com/omarluq/librecode/internal/model"

func (app *App) toggleToolsExpanded() {
	app.toolsExpanded = !app.toolsExpanded
	app.persistSessionSettings()
	app.setStatus("tool output expanded: " + boolText(app.toolsExpanded))
}

func (app *App) toggleThinkingHidden() {
	app.hideThinking = !app.hideThinking
	app.persistSessionSettings()
	app.setStatus("thinking hidden: " + boolText(app.hideThinking))
}

func (app *App) cycleThinking() {
	levels := []model.ThinkingLevel{
		model.ThinkingOff,
		model.ThinkingMinimal,
		model.ThinkingLow,
		model.ThinkingMedium,
		model.ThinkingHigh,
		model.ThinkingXHigh,
	}
	current := app.currentThinkingLevel()
	for index, level := range levels {
		if string(level) == current {
			app.setThinkingLevel(string(levels[(index+1)%len(levels)]))
			return
		}
	}
	app.setThinkingLevel(string(model.ThinkingOff))
}

const boolTextOff = "off"

func boolText(value bool) string {
	if value {
		return "on"
	}

	return boolTextOff
}
