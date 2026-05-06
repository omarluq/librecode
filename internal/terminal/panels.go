package terminal

import (
	"context"
	"sort"
	"strings"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

func (app *App) closePanel() {
	app.mode = modeChat
	app.panel = nil
	app.selectedPanelKind = ""
}

func (app *App) openModelPanel() {
	items := app.modelItems()
	app.openPanel(newSelectionPanel(panelModel, "Select Model", "type to filter; Enter selects", items, true))
}

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

func (app *App) openSessionPanel(ctx context.Context) {
	items, err := app.sessionItems(ctx)
	if err != nil {
		app.addSystemMessage(err.Error())
		return
	}
	panel := newSelectionPanel(panelSessions, "Resume Session", "current working directory", items, true)
	app.openPanel(panel)
}

func (app *App) openTreePanel(ctx context.Context) {
	items, err := app.treeItems(ctx)
	if err != nil {
		app.addSystemMessage(err.Error())
		return
	}
	app.openPanel(newSelectionPanel(panelTree, "Session Tree", "select any entry to branch from it", items, true))
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
	app.openPanel(newSelectionPanel(panelSettings, "Hotkeys", "Pi-compatible default keybindings", items, true))
}

func (app *App) openChangelogPanel() {
	items := []panelItem{
		{
			Value:       "tui",
			Title:       "Pi TUI parity",
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
	app.openPanel(newSelectionPanel(panelSettings, "Changelog", "recent runtime work", items, false))
}

func (app *App) openPanel(panel *selectionPanel) {
	app.mode = modePanel
	app.selectedPanelKind = panel.kind
	app.panel = panel
}

func (app *App) applyPanelSelection(ctx context.Context, value string) error {
	switch app.selectedPanelKind {
	case panelModel:
		app.applyModelSelection(value)
		app.closePanel()
	case panelSettings:
		app.applySettingSelection(value)
	case panelSessions:
		return app.applySessionSelection(ctx, value)
	case panelTree:
		return app.applyTreeSelection(ctx, value)
	}

	return nil
}

func (app *App) modelItems() []panelItem {
	models := app.availableModels()
	items := make([]panelItem, 0, len(models))
	current := modelLabel(app.currentProvider(), app.currentModel())
	for index := range models {
		knownModel := &models[index]
		value := modelLabel(knownModel.Provider, knownModel.ID)
		meta := "[" + knownModel.Provider + "]"
		if value == current {
			meta += " ✓"
		}
		items = append(items, panelItem{Value: value, Title: knownModel.ID, Description: knownModel.Name, Meta: meta})
	}

	return items
}

func (app *App) availableModels() []model.Model {
	models := []model.Model{}
	if app.models != nil {
		models = app.models.Available()
		if len(models) == 0 {
			models = app.models.All()
		}
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

func (app *App) settingsItems() []panelItem {
	return []panelItem{
		{Value: "theme", Title: "Theme", Description: "dark/light visual palette", Meta: app.theme.name},
		{
			Value:       "thinking",
			Title:       "Thinking level",
			Description: "reasoning border color",
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

func (app *App) sessionItems(ctx context.Context) ([]panelItem, error) {
	sessions, err := app.runtime.SessionRepository().ListSessions(ctx, app.cwd)
	if err != nil {
		return nil, err
	}
	items := make([]panelItem, 0, len(sessions))
	for _, session := range sessions {
		title := session.Name
		if title == "" {
			title = session.ID
		}
		meta := session.UpdatedAt.Format("2006-01-02 15:04")
		items = append(items, panelItem{Value: session.ID, Title: title, Description: session.CWD, Meta: meta})
	}

	return items, nil
}

func (app *App) treeItems(ctx context.Context) ([]panelItem, error) {
	if app.sessionID == "" {
		return []panelItem{}, nil
	}
	tree, err := app.runtime.SessionRepository().Tree(ctx, app.sessionID)
	if err != nil {
		return nil, err
	}
	items := []panelItem{}
	appendTreeItems(&items, tree, "")

	return items, nil
}

func appendTreeItems(items *[]panelItem, nodes []database.TreeNodeEntity, prefix string) {
	for index := range nodes {
		node := &nodes[index]
		branch := "├─ "
		nextPrefix := prefix + "│  "
		if index == len(nodes)-1 {
			branch = "└─ "
			nextPrefix = prefix + "   "
		}
		entry := &node.Entry
		title := prefix + branch + string(entry.Type)
		description := treeDescription(entry)
		*items = append(*items, panelItem{
			Value:       entry.ID,
			Title:       title,
			Description: description,
			Meta:        entry.ID,
		})
		appendTreeItems(items, node.Children, nextPrefix)
	}
}

func treeDescription(entry *database.EntryEntity) string {
	if entry.Message.Content != "" {
		return truncateText(entry.Message.Content, 80)
	}
	if entry.Summary != "" {
		return truncateText(entry.Summary, 80)
	}
	if entry.Message.Model != "" {
		return modelLabel(entry.Message.Provider, entry.Message.Model)
	}

	return entry.DataJSON
}

func (app *App) applyModelSelection(value string) {
	provider, modelID, found := strings.Cut(value, "/")
	if !found {
		provider = app.currentProvider()
		modelID = value
	}
	app.setModel(provider, modelID)
}

func (app *App) applySettingSelection(value string) {
	switch value {
	case "theme":
		app.toggleTheme()
	case "thinking":
		app.cycleThinking()
	case "hide-thinking":
		app.hideThinking = !app.hideThinking
	case "tools-expanded":
		app.toolsExpanded = !app.toolsExpanded
	}
	app.panel = newSelectionPanel(
		panelSettings,
		"Settings",
		"Enter cycles values; Esc returns",
		app.settingsItems(),
		false,
	)
}

func (app *App) applySessionSelection(ctx context.Context, value string) error {
	app.sessionID = value
	app.pendingParentID = nil
	app.messages = []chatMessage{}
	app.addSystemMessage("resumed session: " + value)
	if err := app.loadInitialMessages(ctx); err != nil {
		return err
	}
	app.closePanel()

	return nil
}

func (app *App) applyTreeSelection(ctx context.Context, value string) error {
	entry, found, err := app.runtime.SessionRepository().Entry(ctx, app.sessionID, value)
	if err != nil {
		return err
	}
	if !found {
		app.setStatus("entry not found")
		return nil
	}
	app.prepareBranchFromEntry(entry)
	app.closePanel()

	return nil
}

func (app *App) prepareBranchFromEntry(entry *database.EntryEntity) {
	if entry.Message.Role == database.RoleUser || entry.Message.Role == database.RoleCustom {
		app.pendingParentID = emptyParentID(entry.ParentID)
		app.editor.setText(entry.Message.Content)
		app.setStatus("editing selected message to create a branch")
		return
	}
	app.pendingParentID = &entry.ID
	app.editor.setText("")
	app.setStatus("continuing from " + entry.ID)
}

func emptyParentID(parentID *string) *string {
	if parentID == nil {
		root := ""
		return &root
	}

	return parentID
}

func (app *App) toggleTheme() {
	if app.theme.name == "dark" {
		app.theme = lightTheme()
		return
	}
	app.theme = darkTheme()
}

func (app *App) cycleModel(delta int) {
	models := app.availableModels()
	if len(models) == 0 {
		app.setStatus("no models available")
		return
	}
	current := modelLabel(app.currentProvider(), app.currentModel())
	selectedIndex := 0
	for index := range models {
		knownModel := &models[index]
		if modelLabel(knownModel.Provider, knownModel.ID) == current {
			selectedIndex = index
			break
		}
	}
	nextIndex := (selectedIndex + delta + len(models)) % len(models)
	app.setModel(models[nextIndex].Provider, models[nextIndex].ID)
}
