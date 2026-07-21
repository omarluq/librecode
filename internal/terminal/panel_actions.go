package terminal

import (
	"context"
	"fmt"

	"github.com/omarluq/librecode/internal/terminal/panel"
)

func (app *App) closePanel() {
	app.mode = modeChat
	app.panel = nil
	app.selectedPanelKind = ""
}

func (app *App) openPanel(panelModel *panel.Model) {
	app.blurAgentTaskSummary()
	app.blurTranscriptList()
	app.mode = modePanel
	app.selectedPanelKind = panelModel.Kind
	app.panel = panelModel
}

func (app *App) applyPanelSelection(ctx context.Context, value string) error {
	if app.selectedPanelKind == panelWorkflows {
		return app.applyWorkflowSelection(ctx, value)
	}

	return app.applyStandardPanelSelection(ctx, value)
}

func (app *App) applyStandardPanelSelection(ctx context.Context, value string) error {
	switch app.selectedPanelKind {
	case panelAuthLogin, panelAuthLogout:
		return app.applyAuthSelection(ctx, value)
	case panelModel:
		app.applyModelSelection(value)
		app.closePanel()

		return nil
	case panelScopedModels:
		app.toggleScopedModel(value)

		return nil
	case panelSettings:
		app.applySettingSelection(value)

		return nil
	case panelHotkeys, panelChangelog:
		return nil
	case panelSessions:
		return app.applySessionSelection(ctx, value)
	case panelTree:
		return app.applyTreeSelection(ctx, value)
	case panelAgentTasks:
		return app.inspectAgentTask(ctx, value)
	default:
		return fmt.Errorf("unknown panel kind: %q", app.selectedPanelKind)
	}
}
