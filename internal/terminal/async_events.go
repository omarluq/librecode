package terminal

import (
	"context"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

type asyncEventKind string

const (
	asyncEventAuthURL     asyncEventKind = "auth_url"
	asyncEventAuthDone    asyncEventKind = "auth_done"
	asyncEventAuthError   asyncEventKind = "auth_error"
	asyncEventPromptDone  asyncEventKind = "prompt_done"
	asyncEventPromptError asyncEventKind = "prompt_error"
)

type asyncEvent struct {
	Response *assistant.PromptResponse
	Kind     asyncEventKind
	Provider string
	Text     string
}

func (app *App) postAsyncEvent(ctx context.Context, event asyncEvent) {
	defer func() {
		panicValue := recover()
		if panicValue != nil {
			return
		}
	}()
	select {
	case app.screen.EventQ() <- tcell.NewEventInterrupt(event):
	case <-ctx.Done():
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
	case asyncEventPromptDone:
		app.applyPromptResponse(payload.Response)
	case asyncEventPromptError:
		app.working = false
		app.addMessage(database.RoleCustom, payload.Text)
	}

	return false, nil
}
