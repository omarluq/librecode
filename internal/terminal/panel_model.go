package terminal

import (
	"sort"
	"strings"

	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/terminal/panel"
)

func (app *App) openModelPanel() {
	items := app.modelItems()
	app.openPanel(panel.New(panelModel, "Select Model", "type to filter; Enter selects", items, true))
}

func (app *App) modelItems() []panel.Item {
	models := app.availableModels()
	items := make([]panel.Item, 0, len(models))
	current := modelLabel(app.currentProvider(), app.currentModel())

	for index := range models {
		knownModel := &models[index]
		value := modelLabel(knownModel.Provider, knownModel.ID)

		meta := "[" + knownModel.Provider + "]"
		if value == current {
			meta += " ✓"
		}

		items = append(items, panel.Item{Value: value, Title: knownModel.ID, Description: knownModel.Name, Meta: meta})
	}

	return items
}

func (app *App) availableModels() []model.Model {
	models := []model.Model{}
	if app.models != nil {
		models = app.models.Available()
	}

	models = ensureCurrentModel(models, app.currentProvider(), app.currentModel())
	sort.Slice(models, func(leftIndex, rightIndex int) bool {
		left := modelLabel(models[leftIndex].Provider, models[leftIndex].ID)
		right := modelLabel(models[rightIndex].Provider, models[rightIndex].ID)

		return left < right
	})

	return models
}

func ensureCurrentModel(models []model.Model, provider, modelID string) []model.Model {
	for index := range models {
		knownModel := &models[index]
		if knownModel.Provider == provider && knownModel.ID == modelID {
			return models
		}
	}

	current := model.Model{
		ThinkingLevelMap: nil,
		Headers:          nil,
		Compat:           nil,
		Provider:         provider,
		ID:               modelID,
		Name:             modelID,
		API:              "",
		BaseURL:          "",
		Input:            []model.InputMode{model.InputText},
		Cost:             model.Cost{Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0},
		ContextWindow:    0,
		MaxTokens:        0,
		Reasoning:        false,
	}

	return append(models, current)
}

func (app *App) applyModelSelection(value string) {
	provider, modelID, found := strings.Cut(value, "/")
	if !found {
		provider = app.currentProvider()
		modelID = value
	}

	app.setModel(provider, modelID)
}

func (app *App) cycleModel(delta int) {
	modelValues := app.cycleModelValues()
	if len(modelValues) == 0 {
		app.setStatus("no models available")

		return
	}

	current := modelLabel(app.currentProvider(), app.currentModel())
	selectedIndex := 0

	for index, value := range modelValues {
		if value == current {
			selectedIndex = index

			break
		}
	}

	nextIndex := (selectedIndex + delta + len(modelValues)) % len(modelValues)
	app.applyModelSelection(modelValues[nextIndex])
}

func (app *App) cycleModelValues() []string {
	modelValues := app.scopedCycleModels()
	if len(modelValues) > 0 {
		return modelValues
	}

	models := app.availableModels()

	modelValues = make([]string, 0, len(models))
	for index := range models {
		modelValues = append(modelValues, modelLabel(models[index].Provider, models[index].ID))
	}

	return modelValues
}
