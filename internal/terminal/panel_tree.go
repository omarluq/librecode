package terminal

import (
	"context"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/terminal/panel"
	"github.com/omarluq/librecode/internal/tui"
)

func (app *App) openTreePanel(ctx context.Context) {
	items, err := app.treeItems(ctx)
	if err != nil {
		app.addSystemMessage(err.Error())

		return
	}

	app.openPanel(panel.New(panelTree, "Session Tree", "select any entry to branch from it", items, true))
}

func (app *App) treeItems(ctx context.Context) ([]panel.Item, error) {
	if app.sessionID == "" {
		return []panel.Item{}, nil
	}

	tree, err := app.runtime.SessionRepository().Tree(ctx, app.sessionID)
	if err != nil {
		return nil, terminalError(err, "load session tree")
	}

	items := []panel.Item{}
	appendTreeItems(&items, tree, "")

	return items, nil
}

func appendTreeItems(items *[]panel.Item, nodes []database.TreeNodeEntity, prefix string) {
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
		*items = append(*items, panel.Item{
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
		return tui.Truncate(entry.Message.Content, sessionTreePreviewWidth)
	}

	if entry.Summary != "" {
		return tui.Truncate(entry.Summary, sessionTreePreviewWidth)
	}

	if entry.Message.Model != "" {
		return modelLabel(entry.Message.Provider, entry.Message.Model)
	}

	return entry.DataJSON
}

func (app *App) applyTreeSelection(ctx context.Context, value string) error {
	entry, found, err := app.runtime.SessionRepository().Entry(ctx, app.sessionID, value)
	if err != nil {
		return terminalError(err, "load tree entry")
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
		app.resetPromptHistoryNavigation()
		app.composerBuffer.SetText(entry.Message.Content)
		app.setStatus("editing selected message to create a branch")

		return
	}

	app.pendingParentID = &entry.ID
	app.resetPromptHistoryNavigation()
	app.composerBuffer.SetText("")
	app.setStatus("continuing from " + entry.ID)
}

func emptyParentID(parentID *string) *string {
	if parentID == nil {
		root := ""

		return &root
	}

	return parentID
}
