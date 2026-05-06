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
	asyncEventAuthURL          asyncEventKind = "auth_url"
	asyncEventAuthDone         asyncEventKind = "auth_done"
	asyncEventAuthError        asyncEventKind = "auth_error"
	asyncEventPromptDone       asyncEventKind = "prompt_done"
	asyncEventPromptDelta      asyncEventKind = "prompt_delta"
	asyncEventPromptToolStart  asyncEventKind = "prompt_tool_start"
	asyncEventPromptToolResult asyncEventKind = "prompt_tool_result"
	asyncEventPromptError      asyncEventKind = "prompt_error"
)

type asyncEvent struct {
	Response  *assistant.PromptResponse
	ToolEvent *assistant.ToolEvent
	Kind      asyncEventKind
	Provider  string
	Text      string
}

func (app *App) promptStreamHandler(ctx context.Context) func(assistant.StreamEvent) {
	return func(event assistant.StreamEvent) {
		switch event.Kind {
		case assistant.StreamEventTextDelta:
			app.postAsyncEvent(ctx, asyncEvent{
				Response:  nil,
				ToolEvent: nil,
				Kind:      asyncEventPromptDelta,
				Provider:  "",
				Text:      event.Text,
			})
		case assistant.StreamEventToolStart:
			app.postAsyncEvent(ctx, asyncEvent{
				Response:  nil,
				ToolEvent: nil,
				Kind:      asyncEventPromptToolStart,
				Provider:  "",
				Text:      event.Text,
			})
		case assistant.StreamEventToolResult:
			app.postAsyncEvent(ctx, asyncEvent{
				Response:  nil,
				ToolEvent: event.ToolEvent,
				Kind:      asyncEventPromptToolResult,
				Provider:  "",
				Text:      "",
			})
		}
	}
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
	}
}

func (app *App) handleInterrupt(_ context.Context, event *tcell.EventInterrupt) (bool, error) {
	payload, ok := event.Data().(asyncEvent)
	if !ok {
		return false, nil
	}
	if app.handleAuthAsyncEvent(payload) {
		return false, nil
	}
	app.handlePromptAsyncEvent(payload)

	return false, nil
}

func (app *App) handleAuthAsyncEvent(payload asyncEvent) bool {
	switch payload.Kind {
	case asyncEventAuthURL:
		app.addMessage(database.RoleCustom, payload.Text)
		app.setStatus("complete browser login or keep coding")
		return true
	case asyncEventAuthDone:
		app.authWorking = false
		app.refreshModels()
		if payload.Provider == openAICodexProviderID {
			app.setModel(openAICodexProviderID, model.DefaultModelPerProvider[openAICodexProviderID])
		}
		app.addSystemMessage("logged in to " + providerDisplayName(payload.Provider))
		return true
	case asyncEventAuthError:
		app.authWorking = false
		app.addSystemMessage(payload.Text)
		return true
	case asyncEventPromptDone,
		asyncEventPromptDelta,
		asyncEventPromptToolStart,
		asyncEventPromptToolResult,
		asyncEventPromptError:
		return false
	}

	return false
}

func (app *App) handlePromptAsyncEvent(payload asyncEvent) {
	switch payload.Kind {
	case asyncEventPromptDone:
		app.applyPromptResponse(payload.Response)
	case asyncEventPromptDelta:
		app.streamingText += payload.Text
		app.setStatus("streaming response")
	case asyncEventPromptToolStart:
		app.setStatus("running tool: " + payload.Text)
	case asyncEventPromptToolResult:
		app.applyStreamedToolEvent(payload.ToolEvent)
	case asyncEventPromptError:
		app.working = false
		app.streamingText = ""
		app.streamedToolEvents = 0
		app.addMessage(database.RoleCustom, payload.Text)
	case asyncEventAuthURL, asyncEventAuthDone, asyncEventAuthError:
		return
	}
}

func (app *App) applyStreamedToolEvent(event *assistant.ToolEvent) {
	if event == nil {
		return
	}
	app.addMessage(database.RoleToolResult, formatToolEventForUI(event))
	app.streamedToolEvents++
}
