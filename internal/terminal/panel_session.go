package terminal

import (
	"context"

	"github.com/omarluq/librecode/internal/terminal/panel"
	"github.com/omarluq/librecode/internal/tui"
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

func (app *App) sessionItems(ctx context.Context) ([]tui.ListItem, error) {
	sessions, err := app.runtime.SessionRepository().ListSessions(ctx, app.cwd)
	if err != nil {
		return nil, terminalError(err, "list sessions")
	}

	items := make([]tui.ListItem, 0, len(sessions))

	filteredSessions := app.filteredSessionEntities(sessions)
	for index := range filteredSessions {
		session := &filteredSessions[index]
		title := sessionTitle(session)

		description := ""
		if app.sessionShowPath {
			description = session.CWD
		}

		meta := session.UpdatedAt.Format("2006-01-02 15:04")
		items = append(items, tui.ListItem{
			Value:       session.ID,
			Title:       title,
			Description: description,
			Meta:        meta,
		})
	}

	return items, nil
}

func (app *App) applySessionSelection(ctx context.Context, value string) error {
	settings, settingsFound, err := app.sessionSettings(ctx, value)
	if err != nil {
		return terminalError(err, "load session")
	}

	messages, err := app.sessionMessages(ctx, value)
	if err != nil {
		return terminalError(err, "load session")
	}

	app.resetAgentTaskTracking()
	app.agentTaskSessionStack = nil
	app.sessionID = value
	app.pendingParentID = nil
	app.resetMessages()

	if settingsFound {
		app.applySessionSettings(&settings)
	}

	app.appendSessionMessages(messages)
	app.addSystemMessage("resumed session: " + value)
	app.closePanel()

	return nil
}
