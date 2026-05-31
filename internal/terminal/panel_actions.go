package terminal

import "context"

func (app *App) closePanel() {
	app.mode = modeChat
	app.panel = nil
	app.selectedPanelKind = ""
}

func (app *App) openPanel(panel *selectionPanel) {
	app.mode = modePanel
	app.selectedPanelKind = panel.kind
	app.panel = panel
}

func (app *App) applyPanelSelection(ctx context.Context, value string) error {
	switch app.selectedPanelKind {
	case panelAuthLogin, panelAuthLogout:
		return app.applyAuthSelection(ctx, value)
	case panelModel:
		app.applyModelSelection(value)
		app.closePanel()
	case panelScopedModels:
		app.toggleScopedModel(value)
	case panelSettings:
		app.applySettingSelection(value)
	case panelSessions:
		return app.applySessionSelection(ctx, value)
	case panelTree:
		return app.applyTreeSelection(ctx, value)
	}

	return nil
}
