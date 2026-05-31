package terminal

import "context"

func (app *App) openSessionPanel(ctx context.Context) {
	items, err := app.sessionItems(ctx)
	if err != nil {
		app.addSystemMessage(err.Error())
		return
	}
	panel := newSelectionPanel(panelSessions, "Resume Session", app.sessionPanelSubtitle(), items, true)
	app.openPanel(panel)
}

func (app *App) sessionItems(ctx context.Context) ([]panelItem, error) {
	sessions, err := app.runtime.SessionRepository().ListSessions(ctx, app.cwd)
	if err != nil {
		return nil, err
	}
	items := make([]panelItem, 0, len(sessions))
	filteredSessions := app.filteredSessionEntities(sessions)
	for index := range filteredSessions {
		session := &filteredSessions[index]
		title := sessionTitle(session)
		description := ""
		if app.sessionShowPath {
			description = session.CWD
		}
		meta := session.UpdatedAt.Format("2006-01-02 15:04")
		items = append(items, panelItem{
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
		return err
	}
	app.addSystemMessage("resumed session: " + value)
	if err := app.loadInitialMessages(ctx); err != nil {
		return err
	}
	app.closePanel()

	return nil
}
