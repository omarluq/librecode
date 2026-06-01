package terminal

const (
	settingTheme    = "theme"
	settingThinking = "thinking"
	themeNameDark   = "dark"
)

func (app *App) openSettingsPanel() {
	panel := newSelectionPanel(
		panelSettings,
		"Settings",
		"Enter cycles values; Esc returns",
		app.settingsItems(),
		false,
	)
	app.openPanel(panel)
}

func (app *App) openHotkeysPanel() {
	items := make([]panelItem, 0, len(app.keys.rows()))
	for _, row := range app.keys.rows() {
		items = append(items, panelItem{
			Value:       row.Action,
			Title:       row.Keys,
			Description: row.Description,
			Meta:        row.Action,
		})
	}
	app.openPanel(newSelectionPanel(panelHotkeys, "Hotkeys", "librecode default keybindings", items, true))
}

func (app *App) openChangelogPanel() {
	items := []panelItem{
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
			Meta:        "done",
		},
	}
	app.openPanel(newSelectionPanel(panelChangelog, "Changelog", "recent runtime work", items, false))
}

func (app *App) settingsItems() []panelItem {
	return []panelItem{
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
		app.hideThinking = !app.hideThinking
		app.persistSessionSettings()
	case "tools-expanded":
		app.toolsExpanded = !app.toolsExpanded
		app.persistSessionSettings()
	}
	app.panel = newSelectionPanel(
		panelSettings,
		"Settings",
		"Enter cycles values; Esc returns",
		app.settingsItems(),
		false,
	)
}

func (app *App) toggleTheme() {
	if app.theme.name == themeNameDark {
		app.theme = lightTheme()
		app.persistSessionSettings()
		return
	}
	app.theme = darkTheme()
	app.persistSessionSettings()
}
