package terminal

import (
	"context"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

type asyncEventKind string

const (
	asyncEventAuthURL   asyncEventKind = "auth_url"
	asyncEventAuthDone  asyncEventKind = "auth_done"
	asyncEventAuthError asyncEventKind = "auth_error"
)

type asyncEvent struct {
	Kind     asyncEventKind
	Provider string
	Text     string
}

func (app *App) postAsyncEvent(event asyncEvent) {
	select {
	case app.screen.EventQ() <- tcell.NewEventInterrupt(event):
	default:
	}
}

func (app *App) handleInterrupt(_ context.Context, event *tcell.EventInterrupt) (bool, error) {
	payload, ok := event.Data().(asyncEvent)
	if !ok {
		return false, nil
	}
	switch payload.Kind {
	case asyncEventAuthURL:
		app.addMessage(database.RoleCustom, payload.Text)
		app.setStatus("complete browser login or keep coding")
	case asyncEventAuthDone:
		app.authWorking = false
		app.refreshModels()
		if payload.Provider == openAICodexProviderID {
			app.setModel(openAICodexProviderID, model.DefaultModelPerProvider[openAICodexProviderID])
		}
		app.addSystemMessage("logged in to " + providerDisplayName(payload.Provider))
	case asyncEventAuthError:
		app.authWorking = false
		app.addSystemMessage(payload.Text)
	}

	return false, nil
}
