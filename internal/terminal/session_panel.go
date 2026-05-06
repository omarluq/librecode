package terminal

import (
	"context"
	"sort"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/database"
)

func (app *App) handleSessionPanelKey(ctx context.Context, event *tcell.EventKey) bool {
	shortcuts := []shortcutHandler{
		{action: actionSessionTogglePath, handler: app.toggleSessionPath},
		{action: actionSessionToggleSort, handler: app.toggleSessionSort},
		{action: actionSessionToggleNamedFilter, handler: app.toggleNamedSessionFilter},
		{action: actionSessionDelete, handler: func() { app.deleteSelectedSession(ctx) }},
		{action: actionSessionDeleteNoninvasive, handler: func() { app.deleteSelectedSession(ctx) }},
	}
	for _, shortcut := range shortcuts {
		if app.keys.matches(event, shortcut.action) {
			shortcut.handler()
			return true
		}
	}

	return false
}

func (app *App) toggleSessionPath() {
	app.sessionShowPath = !app.sessionShowPath
	app.refreshSessionPanel(context.Background())
}

func (app *App) toggleSessionSort() {
	app.sessionSortRecent = !app.sessionSortRecent
	app.refreshSessionPanel(context.Background())
}

func (app *App) toggleNamedSessionFilter() {
	app.sessionNamedOnly = !app.sessionNamedOnly
	app.refreshSessionPanel(context.Background())
}

func (app *App) deleteSelectedSession(ctx context.Context) {
	value, ok := app.panel.selectedValue()
	if !ok {
		return
	}
	if err := app.runtime.SessionRepository().DeleteSession(ctx, value); err != nil {
		app.setStatus(err.Error())
		return
	}
	if app.sessionID == value {
		app.sessionID = ""
		app.messages = []chatMessage{}
		app.addSystemMessage("deleted active session")
	}
	app.setStatus("deleted session " + value)
	app.refreshSessionPanel(ctx)
}

func (app *App) refreshSessionPanel(ctx context.Context) {
	items, err := app.sessionItems(ctx)
	if err != nil {
		app.setStatus(err.Error())
		return
	}
	app.panel = newSelectionPanel(panelSessions, "Resume Session", app.sessionPanelSubtitle(), items, true)
}

func (app *App) sessionPanelSubtitle() string {
	sortMode := "fuzzy"
	if app.sessionSortRecent {
		sortMode = "recent"
	}
	nameMode := "all"
	if app.sessionNamedOnly {
		nameMode = "named"
	}
	pathMode := "path off"
	if app.sessionShowPath {
		pathMode = "path on"
	}

	return sortMode + " • " + nameMode + " • " + pathMode
}

func (app *App) filteredSessionEntities(sessions []database.SessionEntity) []database.SessionEntity {
	filtered := make([]database.SessionEntity, 0, len(sessions))
	for index := range sessions {
		if app.sessionNamedOnly && sessions[index].Name == "" {
			continue
		}
		filtered = append(filtered, sessions[index])
	}
	if !app.sessionSortRecent {
		sort.Slice(filtered, func(leftIndex, rightIndex int) bool {
			return sessionTitle(&filtered[leftIndex]) < sessionTitle(&filtered[rightIndex])
		})
	}

	return filtered
}

func sessionTitle(session *database.SessionEntity) string {
	if session.Name != "" {
		return session.Name
	}

	return session.ID
}
