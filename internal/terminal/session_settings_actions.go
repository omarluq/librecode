package terminal

import "github.com/omarluq/librecode/internal/model"

func (app *App) setToolsExpanded(expanded bool) {
	app.toolsExpanded = expanded
	app.persistSessionSettings()
}

func (app *App) setHideThinking(hidden bool) {
	app.hideThinking = hidden
	app.persistSessionSettings()
}

func (app *App) setTheme(theme terminalTheme) {
	app.theme = theme
	app.persistSessionSettings()
}

func (app *App) setScopedModelEnabled(value string, enabled bool) {
	app.ensureScopedOrder()
	app.scopedEnabled[value] = enabled
	app.persistSessionSettings()
}

func (app *App) setScopedModelsEnabled(values []string, enabled bool) {
	app.ensureScopedOrder()
	for _, value := range values {
		app.scopedEnabled[value] = enabled
	}
	app.persistSessionSettings()
}

func (app *App) clearScopedModels(values []string) {
	for _, value := range values {
		delete(app.scopedEnabled, value)
	}
	app.persistSessionSettings()
}

func (app *App) setScopedProviderEnabled(provider string, enabled bool) {
	for _, modelItem := range app.scopedModelItems() {
		if providerFromModelValue(modelItem.Value) == provider {
			app.scopedEnabled[modelItem.Value] = enabled
		}
	}
	app.persistSessionSettings()
}

func (app *App) moveScopedModel(value string, delta int) bool {
	app.ensureScopedOrder()
	index := scopedModelIndex(app.scopedOrder, value)
	if index == -1 {
		return false
	}
	nextIndex := index + delta
	if nextIndex < 0 || nextIndex >= len(app.scopedOrder) {
		return false
	}
	app.scopedOrder[index], app.scopedOrder[nextIndex] = app.scopedOrder[nextIndex], app.scopedOrder[index]
	app.persistSessionSettings()

	return true
}

func (app *App) toggleToolsExpanded() {
	next := !app.toolsExpanded
	app.setToolsExpanded(next)
	app.setStatus("tool output expanded: " + boolText(next))
}

func (app *App) toggleThinkingHidden() {
	next := !app.hideThinking
	app.setHideThinking(next)
	app.setStatus("thinking hidden: " + boolText(next))
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
