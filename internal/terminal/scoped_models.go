package terminal

import (
	"strings"

	"github.com/gdamore/tcell/v3"
)

func (app *App) handleScopedModelKey(event *tcell.EventKey) bool {
	shortcuts := []shortcutHandler{
		{action: actionScopedModelsSave, handler: app.saveScopedModels},
		{action: actionScopedModelsEnableAll, handler: app.enableFilteredScopedModels},
		{action: actionScopedModelsClearAll, handler: app.clearFilteredScopedModels},
		{action: actionScopedModelsToggleProvider, handler: app.toggleSelectedProviderModels},
		{action: actionScopedModelsReorderUp, handler: func() { app.reorderSelectedScopedModel(-1) }},
		{action: actionScopedModelsReorderDown, handler: func() { app.reorderSelectedScopedModel(1) }},
	}
	for _, shortcut := range shortcuts {
		if app.keys.matches(event, shortcut.action) {
			shortcut.handler()
			return true
		}
	}

	return false
}

func (app *App) saveScopedModels() {
	app.persistSessionSettings()
	app.setStatus("scoped model cycle saved for this session")
	app.closePanel()
}

func (app *App) enableFilteredScopedModels() {
	values := make([]string, 0, len(app.panel.filtered))
	for _, item := range app.panel.filtered {
		values = append(values, item.Value)
	}
	app.setScopedModelsEnabled(values, true)
	app.refreshScopedModelsPanel()
}

func (app *App) clearFilteredScopedModels() {
	values := make([]string, 0, len(app.panel.filtered))
	for _, item := range app.panel.filtered {
		values = append(values, item.Value)
	}
	app.clearScopedModels(values)
	app.refreshScopedModelsPanel()
}

func (app *App) toggleSelectedProviderModels() {
	item, ok := app.panel.selectedItem()
	if !ok {
		return
	}
	provider := providerFromModelValue(item.Value)
	items := app.scopedModelItems()
	allEnabled := true
	for _, modelItem := range items {
		if providerFromModelValue(modelItem.Value) == provider && !app.scopedEnabled[modelItem.Value] {
			allEnabled = false
			break
		}
	}
	app.setScopedProviderEnabled(provider, !allEnabled)
	app.refreshScopedModelsPanel()
}

func (app *App) reorderSelectedScopedModel(delta int) {
	value, ok := app.panel.selectedValue()
	if !ok {
		return
	}
	if !app.moveScopedModel(value, delta) {
		return
	}
	app.refreshScopedModelsPanel()
}

func (app *App) scopedCycleModels() []string {
	app.ensureScopedOrder()
	models := make([]string, 0, len(app.scopedEnabled))
	for _, value := range app.scopedOrder {
		if app.scopedEnabled[value] {
			models = append(models, value)
		}
	}

	return models
}

func scopedModelIndex(values []string, value string) int {
	for index, candidate := range values {
		if candidate == value {
			return index
		}
	}

	return -1
}

func providerFromModelValue(value string) string {
	provider, _, found := strings.Cut(value, "/")
	if !found {
		return ""
	}

	return provider
}
