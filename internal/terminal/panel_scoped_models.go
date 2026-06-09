package terminal

import (
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/terminal/panel"
)

func (app *App) openScopedModelsPanel() {
	app.openPanel(panel.New(
		panelScopedModels,
		"Scoped Models",
		"Enter toggles; Ctrl+S saves; Ctrl+A all; Ctrl+X clear; Ctrl+P provider",
		app.scopedModelItems(),
		true,
	))
}

func (app *App) scopedModelItems() []panel.Item {
	models := app.orderedAvailableModels()
	items := make([]panel.Item, 0, len(models))
	for index := range models {
		knownModel := &models[index]
		value := modelLabel(knownModel.Provider, knownModel.ID)
		check := "☐"
		if app.scopedEnabled[value] {
			check = "☑"
		}
		items = append(items, panel.Item{
			Value:       value,
			Title:       check + " " + knownModel.ID,
			Description: knownModel.Name,
			Meta:        "[" + knownModel.Provider + "]",
		})
	}

	return items
}

func (app *App) orderedAvailableModels() []model.Model {
	models := app.availableModels()
	if len(app.scopedOrder) == 0 {
		return models
	}
	byValue := make(map[string]model.Model, len(models))
	for index := range models {
		byValue[modelLabel(models[index].Provider, models[index].ID)] = models[index]
	}
	ordered := make([]model.Model, 0, len(models))
	seen := map[string]bool{}
	for _, value := range app.scopedOrder {
		knownModel, ok := byValue[value]
		if ok {
			ordered = append(ordered, knownModel)
			seen[value] = true
		}
	}
	for index := range models {
		value := modelLabel(models[index].Provider, models[index].ID)
		if !seen[value] {
			ordered = append(ordered, models[index])
		}
	}

	return ordered
}

func (app *App) toggleScopedModel(value string) {
	app.setScopedModelEnabled(value, !app.scopedEnabled[value])
	app.refreshScopedModelsPanel()
}

func (app *App) ensureScopedOrder() {
	if len(app.scopedOrder) > 0 {
		return
	}
	models := app.availableModels()
	app.scopedOrder = make([]string, 0, len(models))
	for index := range models {
		app.scopedOrder = append(app.scopedOrder, modelLabel(models[index].Provider, models[index].ID))
	}
}

func (app *App) refreshScopedModelsPanel() {
	if app.selectedPanelKind != panelScopedModels {
		return
	}
	app.panel = panel.New(
		panelScopedModels,
		"Scoped Models",
		"Enter toggles; Ctrl+S saves; Ctrl+A all; Ctrl+X clear; Ctrl+P provider",
		app.scopedModelItems(),
		true,
	)
}
