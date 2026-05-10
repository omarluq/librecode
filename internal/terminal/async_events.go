package terminal

import (
	"context"
	"time"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

type asyncEventKind string

const (
	asyncEventAuthURL             asyncEventKind = "auth_url"
	asyncEventAuthDone            asyncEventKind = "auth_done"
	asyncEventAuthError           asyncEventKind = "auth_error"
	asyncEventPromptDone          asyncEventKind = "prompt_done"
	asyncEventPromptUserEntry     asyncEventKind = "prompt_user_entry"
	asyncEventPromptDelta         asyncEventKind = "prompt_delta"
	asyncEventPromptThinkingDelta asyncEventKind = "prompt_thinking_delta"
	asyncEventPromptToolStart     asyncEventKind = "prompt_tool_start"
	asyncEventPromptToolResult    asyncEventKind = "prompt_tool_result"
	asyncEventPromptRetry         asyncEventKind = "prompt_retry"
	asyncEventPromptError         asyncEventKind = "prompt_error"
)

type asyncEvent struct {
	Response  *assistant.PromptResponse
	ToolEvent *assistant.ToolEvent
	Kind      asyncEventKind
	Provider  string
	Text      string
	PromptID  uint64
}

func (app *App) promptUserEntryHandler(ctx context.Context, promptID uint64) func(assistant.PromptUserEntryEvent) {
	return func(event assistant.PromptUserEntryEvent) {
		app.postAsyncEvent(ctx, asyncEvent{
			Response:  nil,
			ToolEvent: nil,
			Kind:      asyncEventPromptUserEntry,
			Provider:  event.SessionID,
			Text:      event.EntryID,
			PromptID:  promptID,
		})
	}
}

func (app *App) promptRetryHandler(ctx context.Context, promptID uint64) assistant.RetryEventHandler {
	return func(event assistant.RetryEvent) {
		text := "retrying model request"
		if event.Kind == assistant.RetryEventStart {
			text = "retrying model request in " + event.Delay.Round(time.Second).String()
		}
		app.postAsyncEvent(ctx, asyncEvent{
			Response:  nil,
			ToolEvent: nil,
			Kind:      asyncEventPromptRetry,
			Provider:  string(event.Kind),
			Text:      text,
			PromptID:  promptID,
		})
	}
}

func (app *App) promptStreamHandler(ctx context.Context, promptID uint64) func(assistant.StreamEvent) {
	return func(event assistant.StreamEvent) {
		switch event.Kind {
		case assistant.StreamEventTextDelta:
			app.postAsyncEvent(ctx, asyncEvent{
				Response:  nil,
				ToolEvent: nil,
				Kind:      asyncEventPromptDelta,
				Provider:  "",
				Text:      event.Text,
				PromptID:  promptID,
			})
		case assistant.StreamEventThinkingDelta:
			app.postAsyncEvent(ctx, asyncEvent{
				Response:  nil,
				ToolEvent: nil,
				Kind:      asyncEventPromptThinkingDelta,
				Provider:  "",
				Text:      event.Text,
				PromptID:  promptID,
			})
		case assistant.StreamEventToolStart:
			app.postAsyncEvent(ctx, asyncEvent{
				Response:  nil,
				ToolEvent: nil,
				Kind:      asyncEventPromptToolStart,
				Provider:  "",
				Text:      event.Text,
				PromptID:  promptID,
			})
		case assistant.StreamEventToolResult:
			app.postAsyncEvent(ctx, asyncEvent{
				Response:  nil,
				ToolEvent: event.ToolEvent,
				Kind:      asyncEventPromptToolResult,
				Provider:  "",
				Text:      "",
				PromptID:  promptID,
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

func (app *App) handleInterrupt(ctx context.Context, event *tcell.EventInterrupt) (bool, error) {
	payload, ok := event.Data().(asyncEvent)
	if !ok {
		return false, nil
	}
	if app.handleAuthAsyncEvent(payload) {
		return false, nil
	}
	app.handlePromptAsyncEvent(ctx, payload)

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
		asyncEventPromptUserEntry,
		asyncEventPromptDelta,
		asyncEventPromptThinkingDelta,
		asyncEventPromptToolStart,
		asyncEventPromptToolResult,
		asyncEventPromptRetry,
		asyncEventPromptError:
		return false
	}

	return false
}

func (app *App) handlePromptAsyncEvent(ctx context.Context, payload asyncEvent) {
	if app.ignorePromptEvent(payload) {
		return
	}
	if app.handlePromptLifecycleEvent(ctx, payload) {
		return
	}
	app.handlePromptStreamEvent(ctx, payload)
}

func (app *App) ignorePromptEvent(payload asyncEvent) bool {
	if !isPromptAsyncEvent(payload.Kind) {
		return false
	}
	if app.activePrompt != nil && app.activePrompt.ID == payload.PromptID {
		return false
	}
	_, waitingForCleanup := app.canceledPrompts[payload.PromptID]

	return !waitingForCleanup
}

func isPromptAsyncEvent(kind asyncEventKind) bool {
	switch kind {
	case asyncEventPromptDone,
		asyncEventPromptUserEntry,
		asyncEventPromptDelta,
		asyncEventPromptThinkingDelta,
		asyncEventPromptToolStart,
		asyncEventPromptToolResult,
		asyncEventPromptRetry,
		asyncEventPromptError:
		return true
	case asyncEventAuthURL, asyncEventAuthDone, asyncEventAuthError:
		return false
	}

	return false
}

func (app *App) handlePromptLifecycleEvent(ctx context.Context, payload asyncEvent) bool {
	switch payload.Kind {
	case asyncEventPromptDone:
		app.emitExtensionRuntimeEventOrMessage(ctx, extensionEventPromptDone, promptDoneExtensionData(payload.Response))
		app.applyPromptResponse(ctx, payload.Response, payload.PromptID)
		return true
	case asyncEventPromptUserEntry:
		data := map[string]any{
			extensionDataSessionID: payload.Provider,
			extensionDataEntryID:   payload.Text,
			extensionDataPromptID:  payload.PromptID,
		}
		app.emitExtensionRuntimeEventOrMessage(ctx, extensionEventPromptUser, data)
		app.applyPromptUserEntry(ctx, payload.Provider, payload.Text, payload.PromptID)
		return true
	case asyncEventPromptRetry:
		app.emitPromptRetryExtensionEvent(ctx, payload)
		app.setStatus(payload.Text)
		return true
	case asyncEventPromptError:
		app.applyPromptError(payload.Text, payload.PromptID)
		return true
	case asyncEventAuthURL, asyncEventAuthDone, asyncEventAuthError:
		return true
	case asyncEventPromptDelta, asyncEventPromptThinkingDelta, asyncEventPromptToolStart, asyncEventPromptToolResult:
		return false
	}

	return false
}

func (app *App) handlePromptStreamEvent(ctx context.Context, payload asyncEvent) {
	if app.activePrompt != nil && app.activePrompt.Canceled {
		return
	}
	switch payload.Kind {
	case asyncEventPromptDelta:
		app.emitExtensionRuntimeEventOrMessage(
			ctx,
			extensionEventModelDelta,
			map[string]any{extensionDataText: payload.Text},
		)
		app.appendStreamingBlock(database.RoleAssistant, payload.Text)
	case asyncEventPromptThinkingDelta:
		app.emitExtensionRuntimeEventOrMessage(
			ctx,
			extensionEventThinkingDelta,
			map[string]any{extensionDataText: payload.Text},
		)
		app.appendStreamingBlock(database.RoleThinking, payload.Text)
	case asyncEventPromptToolResult:
		app.emitExtensionRuntimeEventOrMessage(ctx, extensionEventToolEnd, toolExtensionData(payload.ToolEvent))
		app.applyStreamedToolEvent(payload.ToolEvent)
	case asyncEventPromptToolStart:
		app.emitExtensionRuntimeEventOrMessage(
			ctx,
			extensionEventToolStart,
			map[string]any{extensionDataName: payload.Text},
		)
		return
	case asyncEventPromptDone,
		asyncEventPromptUserEntry,
		asyncEventPromptRetry,
		asyncEventPromptError,
		asyncEventAuthURL,
		asyncEventAuthDone,
		asyncEventAuthError:
		return
	}
}

func (app *App) emitPromptRetryExtensionEvent(ctx context.Context, payload asyncEvent) {
	eventName := extensionEventRetryStart
	if payload.Provider == string(assistant.RetryEventEnd) {
		eventName = extensionEventRetryEnd
	}
	app.emitExtensionRuntimeEventOrMessage(ctx, eventName, map[string]any{
		extensionDataPromptID: payload.PromptID,
		extensionDataText:     payload.Text,
	})
}

func promptDoneExtensionData(response *assistant.PromptResponse) map[string]any {
	if response == nil {
		return map[string]any{}
	}

	return map[string]any{
		extensionDataSessionID:        response.SessionID,
		extensionDataUserEntryID:      response.UserEntryID,
		extensionDataAssistantEntryID: response.AssistantEntryID,
		extensionDataText:             response.Text,
		extensionDataCached:           response.Cached,
	}
}

func toolExtensionData(event *assistant.ToolEvent) map[string]any {
	if event == nil {
		return map[string]any{}
	}

	return map[string]any{
		extensionDataName:         event.Name,
		extensionDataToolArgsJSON: event.ArgumentsJSON,
		extensionDataDetailsJSON:  event.DetailsJSON,
		extensionDataResult:       event.Result,
		extensionDataError:        event.Error,
	}
}

func (app *App) applyPromptUserEntry(ctx context.Context, sessionID, entryID string, promptID uint64) {
	if canceledPrompt, ok := app.canceledPrompts[promptID]; ok {
		canceledPrompt.SessionID = sessionID
		canceledPrompt.UserEntryID = entryID
		app.deleteCanceledPromptBranch(ctx, canceledPrompt)
		return
	}
	if app.activePrompt == nil || app.activePrompt.ID != promptID {
		return
	}
	app.activePrompt.SessionID = sessionID
	app.activePrompt.UserEntryID = entryID
	if app.activePrompt.Canceled {
		app.deleteCanceledPromptBranch(ctx, app.activePrompt)
	}
}

func (app *App) applyPromptError(message string, promptID uint64) {
	if app.consumeCanceledPrompt(promptID) {
		app.setStatus("response canceled; conversation reverted")
		return
	}
	app.working = false
	app.streamingText = ""
	app.streamingThinkingText = ""
	app.resetStreamingBlocks()
	app.streamedToolEvents = 0
	if app.activePrompt != nil && app.activePrompt.Canceled {
		app.activePrompt = nil
		app.setStatus("response canceled; conversation reverted")
		return
	}
	app.activePrompt = nil
	app.addMessage(database.RoleCustom, message)
}

func (app *App) consumeCanceledPrompt(promptID uint64) bool {
	if _, ok := app.canceledPrompts[promptID]; !ok {
		return false
	}
	delete(app.canceledPrompts, promptID)
	if app.activePrompt != nil && app.activePrompt.ID == promptID {
		app.activePrompt = nil
	}

	return true
}

func (app *App) applyStreamedToolEvent(event *assistant.ToolEvent) {
	if event == nil {
		return
	}
	app.appendStreamingBlock(database.RoleToolResult, formatToolEventForUI(event))
	app.streamedToolEvents++
}

func (app *App) appendStreamingBlock(role database.Role, content string) {
	if content == "" {
		return
	}
	lastIndex := len(app.streamingBlocks) - 1
	if lastIndex >= 0 && canMergeStreamingBlock(role) && app.streamingBlocks[lastIndex].Role == role {
		app.streamingBlocks[lastIndex].Content += content
		app.invalidateStreamingBlockCache(lastIndex)
		return
	}
	app.streamingBlocks = append(app.streamingBlocks, newChatMessage(role, content))
	if len(app.streamingBlockLineCache) > 0 {
		app.streamingBlockLineCache = append(app.streamingBlockLineCache, emptyCachedRenderedMessage())
	}
}

func (app *App) invalidateStreamingBlockCache(index int) {
	if index >= 0 && index < len(app.streamingBlockLineCache) {
		app.streamingBlockLineCache[index] = emptyCachedRenderedMessage()
	}
}

func canMergeStreamingBlock(role database.Role) bool {
	switch role {
	case database.RoleAssistant, database.RoleThinking:
		return true
	case database.RoleUser,
		database.RoleToolResult,
		database.RoleBashExecution,
		database.RoleCustom,
		database.RoleBranchSummary,
		database.RoleCompactionSummary:
		return false
	}

	return false
}
