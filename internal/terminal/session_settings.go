package terminal

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

const sessionSettingsNamespace = "terminal_session_settings"

type sessionSettingsDocument struct {
	Provider      string   `json:"provider"`
	Model         string   `json:"model"`
	ThinkingLevel string   `json:"thinking_level"`
	Theme         string   `json:"theme"`
	ScopedEnabled []string `json:"scoped_enabled"`
	ScopedOrder   []string `json:"scoped_order"`
	HideThinking  bool     `json:"hide_thinking"`
	ToolsExpanded bool     `json:"tools_expanded"`
}

func (app *App) loadSessionSettings(ctx context.Context) error {
	if app.sessionID == "" {
		return nil
	}

	return app.loadSettingsForSession(ctx, app.sessionID)
}

func (app *App) loadLatestSessionSettings(ctx context.Context) error {
	if app.sessionID != "" || app.runtime == nil {
		return nil
	}

	latestSession, found, err := app.runtime.SessionRepository().LatestSession(ctx, app.cwd)
	if err != nil || !found {
		return terminalError(err, "load latest session")
	}

	return app.loadSettingsForSession(ctx, latestSession.ID)
}

func (app *App) loadSettingsForSession(ctx context.Context, sessionID string) error {
	if app.settings == nil || sessionID == "" {
		return nil
	}

	document, found, err := app.settings.Get(ctx, sessionSettingsNamespace, sessionID)
	if err != nil || !found {
		return terminalError(err, "load session settings")
	}

	settings := sessionSettingsDocument{
		Provider:      "",
		Model:         "",
		ThinkingLevel: "",
		Theme:         "",
		ScopedEnabled: nil,
		ScopedOrder:   nil,
		HideThinking:  false,
		ToolsExpanded: false,
	}
	if err := json.Unmarshal([]byte(document.ValueJSON), &settings); err != nil {
		return terminalError(err, "decode session settings")
	}

	app.applySessionSettings(&settings)

	return nil
}

func (app *App) saveSessionSettings(ctx context.Context) error {
	if app.settings == nil || app.sessionID == "" {
		return nil
	}

	encoded, err := json.Marshal(app.currentSessionSettings())
	if err != nil {
		return terminalError(err, "encode session settings")
	}

	err = app.settings.Put(ctx, &database.DocumentEntity{
		UpdatedAt: time.Now().UTC(),
		Namespace: sessionSettingsNamespace,
		Key:       app.sessionID,
		ValueJSON: string(encoded),
	})

	return terminalError(err, "save session settings")
}

func (app *App) persistSessionSettings() {
	if err := app.saveSessionSettings(context.Background()); err != nil {
		app.setStatus(err.Error())
	}
}

func (app *App) currentSessionSettings() sessionSettingsDocument {
	return sessionSettingsDocument{
		Provider:      app.currentProvider(),
		Model:         app.currentModel(),
		ThinkingLevel: app.currentThinkingLevel(),
		Theme:         app.theme.name,
		ScopedEnabled: app.scopedEnabledValues(),
		ScopedOrder:   append([]string{}, app.scopedOrder...),
		HideThinking:  app.hideThinking,
		ToolsExpanded: app.toolsExpanded,
	}
}

func (app *App) currentThinkingLevel() string {
	if app.cfg == nil || app.cfg.Assistant.ThinkingLevel == "" {
		return string(model.ThinkingOff)
	}

	return app.cfg.Assistant.ThinkingLevel
}

func (app *App) currentProvider() string {
	if app.cfg == nil {
		return "local"
	}

	return app.cfg.Assistant.Provider
}

func (app *App) currentModel() string {
	if app.cfg == nil {
		return "librecode"
	}

	return app.cfg.Assistant.Model
}

func (app *App) scopedEnabledValues() []string {
	values := make([]string, 0, len(app.scopedEnabled))
	for value, enabled := range app.scopedEnabled {
		if enabled {
			values = append(values, value)
		}
	}

	sort.Strings(values)

	return values
}

func (app *App) applySessionSettings(settings *sessionSettingsDocument) {
	if app.cfg != nil {
		if settings.Provider != "" {
			app.cfg.Assistant.Provider = settings.Provider
		}

		if settings.Model != "" {
			app.cfg.Assistant.Model = settings.Model
		}

		if settings.ThinkingLevel != "" {
			app.cfg.Assistant.ThinkingLevel = settings.ThinkingLevel
		}
	}

	if settings.Theme != "" {
		app.theme = themeByName(settings.Theme)
	}

	app.hideThinking = settings.HideThinking
	app.toolsExpanded = settings.ToolsExpanded
	app.scopedOrder = append([]string{}, settings.ScopedOrder...)

	app.scopedEnabled = map[string]bool{}
	for _, value := range settings.ScopedEnabled {
		app.scopedEnabled[value] = true
	}
}

func (app *App) setModelSelection(provider, modelID string) {
	if app.cfg != nil {
		app.cfg.Assistant.Provider = provider
		app.cfg.Assistant.Model = modelID
	}

	app.persistSessionSettings()
}

func (app *App) setThinkingLevelValue(level string) {
	if app.cfg != nil {
		app.cfg.Assistant.ThinkingLevel = level
	}

	app.persistSessionSettings()
}
