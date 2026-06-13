package terminal

import (
	"context"

	"github.com/omarluq/librecode/internal/terminal/panel"
)

func (app *App) openSessionPanel(ctx context.Context) {
	items, err := app.sessionItems(ctx)
	if err != nil {
		app.addSystemMessage(err.Error())

		return
	}

	model := panel.New(panelSessions, "Resume Session", app.sessionPanelSubtitle(), items, true)
	app.openPanel(model)
}

func (app *App) sessionItems(ctx context.Context) ([]panel.Item, error) {
	sessions, err := app.runtime.SessionRepository().ListSessions(ctx, app.cwd)
	if err != nil {
		return nil, terminalError(err, "list sessions")
	}

	items := make([]panel.Item, 0, len(sessions))

	filteredSessions := app.filteredSessionEntities(sessions)
	for index := range filteredSessions {
		session := &filteredSessions[index]
		title := sessionTitle(session)

		description := ""
		if app.sessionShowPath {
			description = session.CWD
		}

		meta := session.UpdatedAt.Format("2006-01-02 15:04")
		items = append(items, panel.Item{
			Value:       session.ID,
			Title:       title,
			Description: description,
			Meta:        meta,
		})
	}

	return items, nil
}

func (app *App) applySessionSelection(ctx context.Context, value string) error {
	app.sessionID = value
	app.pendingParentID = nil
	app.resetMessages()

	if err := app.loadSessionSettings(ctx); err != nil {
		return terminalError(err, "load session")
	}

	if err := app.loadInitialMessages(ctx); err != nil {
		return terminalError(err, "load session")
	}

	app.addSystemMessage("resumed session: " + value)
	app.closePanel()

	return nil
}
