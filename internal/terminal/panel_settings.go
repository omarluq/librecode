package terminal

import (
	"github.com/omarluq/librecode/internal/terminal/panel"
	"github.com/omarluq/librecode/internal/tui"
)

const (
	settingTheme    = "theme"
	settingThinking = "thinking"
	statusDone      = "done"
	themeNameDark   = "dark"
)

func (app *App) openSettingsPanel() {
	model := panel.New(
		panelSettings,
		"Settings",
		"Enter cycles values; Esc returns",
		app.settingsItems(),
		false,
	)
	app.openPanel(model)
}

func (app *App) openHotkeysPanel() {
	items := make([]tui.ListItem, 0, len(app.keys.rows()))
	for _, row := range app.keys.rows() {
		items = append(items, tui.ListItem{
			Value:       row.Action,
			Title:       row.Keys,
			Description: row.Description,
			Meta:        row.Action,
		})
	}

	app.openPanel(panel.New(panelHotkeys, "Hotkeys", "librecode default keybindings", items, true))
}

func (app *App) openChangelogPanel() {
	items := []tui.ListItem{
		{
			Value:       "tui",
			Title:       "TUI parity",
			Description: "Theme, keybindings, panels, footer, and session tree",
			Meta:        "now",
		},
		{
			Value:       "db",
			Title:       "Database sessions",
			Description: "Session entries and normalized messages are SQLite-backed",
			Meta:        statusDone,
		},
	}
	app.openPanel(panel.New(panelChangelog, "Changelog", "recent runtime work", items, false))
}

func (app *App) settingsItems() []tui.ListItem {
	return []tui.ListItem{
		{Value: settingTheme, Title: "Theme", Description: "dark/light visual palette", Meta: app.theme.name},
		{
			Value:       settingThinking,
			Title:       "Thinking level",
			Description: "model reasoning effort",
			Meta:        app.currentThinkingLevel(),
		},
		{
			Value:       "hide-thinking",
			Title:       "Hide thinking",
			Description: "collapse thinking blocks",
			Meta:        boolText(app.hideThinking),
		},
		{
			Value:       "tools-expanded",
			Title:       "Tool output",
			Description: "collapse or expand tool output",
			Meta:        boolText(app.toolsExpanded),
		},
	}
}

func (app *App) applySettingSelection(value string) {
	switch value {
	case settingTheme:
		app.toggleTheme()
	case settingThinking:
		app.cycleThinking()
	case "hide-thinking":
		app.setHideThinking(!app.hideThinking)
	case "tools-expanded":
		app.setToolsExpanded(!app.toolsExpanded)
	}

	app.panel = panel.New(
		panelSettings,
		"Settings",
		"Enter cycles values; Esc returns",
		app.settingsItems(),
		false,
	)
}

func (app *App) toggleTheme() {
	if app.theme.name == themeNameDark {
		app.setTheme(lightTheme())

		return
	}

	app.setTheme(darkTheme())
}
