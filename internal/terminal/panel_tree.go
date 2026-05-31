package terminal

import (
	"context"

	"github.com/omarluq/librecode/internal/database"
)

func (app *App) openTreePanel(ctx context.Context) {
	items, err := app.treeItems(ctx)
	if err != nil {
		app.addSystemMessage(err.Error())
		return
	}
	app.openPanel(newSelectionPanel(panelTree, "Session Tree", "select any entry to branch from it", items, true))
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
		app.resetPromptHistoryNavigation()
		app.setComposerText(entry.Message.Content)
		app.setStatus("editing selected message to create a branch")
		return
	}
	app.pendingParentID = &entry.ID
	app.resetPromptHistoryNavigation()
	app.setComposerText("")
	app.setStatus("continuing from " + entry.ID)
}

func emptyParentID(parentID *string) *string {
	if parentID == nil {
		root := ""
		return &root
	}

	return parentID
}
