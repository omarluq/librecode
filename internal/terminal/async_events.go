package terminal

import (
	"context"
	"strings"
	"time"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/tool"
	"github.com/omarluq/librecode/internal/transcript"
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
	asyncEventPromptUsage         asyncEventKind = "prompt_usage"
	asyncEventPromptUsageSnapshot asyncEventKind = "prompt_usage_snapshot"
	asyncEventPromptError         asyncEventKind = "prompt_error"
	asyncEventPromptContext       asyncEventKind = "prompt_context"
	asyncEventCompactStart        asyncEventKind = "compact_start"
	asyncEventCompactDone         asyncEventKind = "compact_done"
	asyncEventCompactError        asyncEventKind = "compact_error"
	asyncEventAgentTaskChanged    asyncEventKind = "agent_task_changed"
	asyncEventAgentTaskCompleted  asyncEventKind = "agent_task_completed"
)

type asyncEvent struct {
	Response      *assistant.PromptResponse
	ToolCallEvent *assistant.ToolCallEvent
	ToolEvent     *assistant.ToolEvent
	Usage         *model.TokenUsage
	Kind          asyncEventKind
	Provider      string
	Text          string
	PromptID      uint64
}

func (app *App) promptUserEntryHandler(ctx context.Context, promptID uint64) func(assistant.PromptUserEntryEvent) {
	return func(event assistant.PromptUserEntryEvent) {
		app.postAsyncEvent(ctx, &asyncEvent{
			Response:      nil,
			ToolCallEvent: nil,
			ToolEvent:     nil,
			Usage:         nil,
			Kind:          asyncEventPromptUserEntry,
			Provider:      event.SessionID,
			Text:          event.EntryID,
			PromptID:      promptID,
		})
	}
}

func (app *App) promptRetryHandler(ctx context.Context, promptID uint64) assistant.RetryEventHandler {
	return func(event assistant.RetryEvent) {
		text := "retrying model request"
		if event.Kind == assistant.RetryEventStart {
			text = "retrying model request in " + event.Delay.Round(time.Second).String()
		}

		app.postAsyncEvent(ctx, &asyncEvent{
			Response:      nil,
			ToolCallEvent: nil,
			ToolEvent:     nil,
			Usage:         nil,
			Kind:          asyncEventPromptRetry,
			Provider:      string(event.Kind),
			Text:          text,
			PromptID:      promptID,
		})
	}
}

func (app *App) promptStreamHandler(ctx context.Context, promptID uint64) func(assistant.StreamEvent) {
	return func(event assistant.StreamEvent) {
		payload, ok := asyncEventFromStreamEvent(event, promptID)
		if !ok {
			return
		}

		app.postAsyncEvent(ctx, payload)
	}
}

func asyncEventFromStreamEvent(event assistant.StreamEvent, promptID uint64) (*asyncEvent, bool) {
	payload := &asyncEvent{
		Response:      nil,
		ToolCallEvent: nil,
		ToolEvent:     nil,
		Usage:         nil,
		Kind:          asyncEventPromptDelta,
		Provider:      "",
		Text:          event.Text,
		PromptID:      promptID,
	}

	switch event.Kind {
	case assistant.StreamEventTextDelta:
		payload.Kind = asyncEventPromptDelta
	case assistant.StreamEventThinkingDelta:
		payload.Kind = asyncEventPromptThinkingDelta
	case assistant.StreamEventToolStart:
		payload.ToolCallEvent = event.ToolCallEvent
		payload.Kind = asyncEventPromptToolStart
	case assistant.StreamEventToolResult, assistant.StreamEventSkillLoaded:
		payload.ToolEvent = event.ToolEvent
		payload.Text = ""
		payload.Kind = asyncEventPromptToolResult
	case assistant.StreamEventUsage:
		payload.Usage = event.Usage
		payload.Text = ""
		payload.Kind = asyncEventPromptUsage
	case assistant.StreamEventUsageSnapshot:
		payload.Usage = event.Usage
		payload.Text = ""
		payload.Kind = asyncEventPromptUsageSnapshot
	case assistant.StreamEventContextCompaction,
		assistant.StreamEventContextCompactionStart,
		assistant.StreamEventContextCompactionDone,
		assistant.StreamEventContextCompactionError:
		payload.Kind = asyncContextEventKind(event.Kind)
	case assistant.StreamEventUnknown:
		return nil, false
	default:
		return nil, false
	}

	return payload, true
}

func (app *App) postAsyncEvent(ctx context.Context, event *asyncEvent) {
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
	payload, ok := event.Data().(*asyncEvent)
	if !ok {
		return false, nil
	}

	if app.handleAuthAsyncEvent(payload) {
		return false, nil
	}

	if app.handleCompactAsyncEvent(ctx, payload) {
		return false, nil
	}

	app.handlePromptAsyncEvent(ctx, payload)

	return false, nil
}

func (app *App) handleAuthAsyncEvent(payload *asyncEvent) bool {
	switch payload.Kind {
	case asyncEventAuthURL:
		app.addMessage(transcript.RoleCustom, payload.Text)
		app.setStatus("complete browser login or keep coding")

		return true
	case asyncEventAuthDone:
		app.authWorking = false
		app.refreshModels()
		app.selectProviderDefault(payload.Provider)
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
		asyncEventPromptUsage,
		asyncEventPromptUsageSnapshot,
		asyncEventPromptError,
		asyncEventPromptContext,
		asyncEventAgentTaskChanged,
		asyncEventAgentTaskCompleted,
		asyncEventCompactStart,
		asyncEventCompactDone,
		asyncEventCompactError:
		return false
	}

	return false
}

func (app *App) handlePromptAsyncEvent(ctx context.Context, payload *asyncEvent) {
	switch payload.Kind {
	case asyncEventAgentTaskChanged:
		app.refreshVisibleAgentTasks(ctx)

		return
	case asyncEventAgentTaskCompleted:
		app.deliverAgentTaskCompletionText(ctx, payload.Provider, payload.Text)

		return
	case asyncEventAuthURL,
		asyncEventAuthDone,
		asyncEventAuthError,
		asyncEventPromptDone,
		asyncEventPromptUserEntry,
		asyncEventPromptDelta,
		asyncEventPromptThinkingDelta,
		asyncEventPromptToolStart,
		asyncEventPromptToolResult,
		asyncEventPromptRetry,
		asyncEventPromptUsage,
		asyncEventPromptUsageSnapshot,
		asyncEventPromptError,
		asyncEventPromptContext,
		asyncEventCompactStart,
		asyncEventCompactDone,
		asyncEventCompactError:
	}

	if app.ignorePromptEvent(payload) {
		return
	}

	if app.handlePromptLifecycleEvent(ctx, payload) {
		return
	}

	app.handlePromptStreamEvent(ctx, payload)
}

func (app *App) ignorePromptEvent(payload *asyncEvent) bool {
	if !isPromptAsyncEvent(payload.Kind) {
		return false
	}

	if isCompactionAsyncEvent(payload.Kind) {
		return false
	}

	return app.activePrompt == nil || app.activePrompt.ID != payload.PromptID
}

func isCompactionAsyncEvent(kind asyncEventKind) bool {
	switch kind {
	case asyncEventCompactStart,
		asyncEventCompactDone,
		asyncEventCompactError:
		return true
	case asyncEventAuthURL,
		asyncEventAuthDone,
		asyncEventAuthError,
		asyncEventPromptDone,
		asyncEventPromptUserEntry,
		asyncEventPromptDelta,
		asyncEventPromptThinkingDelta,
		asyncEventPromptToolStart,
		asyncEventPromptToolResult,
		asyncEventPromptRetry,
		asyncEventPromptUsage,
		asyncEventPromptUsageSnapshot,
		asyncEventPromptError,
		asyncEventPromptContext,
		asyncEventAgentTaskChanged,
		asyncEventAgentTaskCompleted:
		return false
	}

	return false
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
		asyncEventPromptUsage,
		asyncEventPromptUsageSnapshot,
		asyncEventPromptError,
		asyncEventPromptContext,
		asyncEventCompactStart,
		asyncEventCompactDone,
		asyncEventCompactError:
		return true
	case asyncEventAuthURL,
		asyncEventAuthDone,
		asyncEventAuthError,
		asyncEventAgentTaskChanged,
		asyncEventAgentTaskCompleted:
		return false
	}

	return false
}

func (app *App) handlePromptLifecycleEvent(ctx context.Context, payload *asyncEvent) bool {
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
		app.applyPromptError(ctx, payload.Text, payload.PromptID)

		return true
	case asyncEventPromptContext,
		asyncEventCompactStart,
		asyncEventCompactDone,
		asyncEventCompactError:
		app.applyPromptContextEvent(payload)

		return true
	case asyncEventAuthURL,
		asyncEventAuthDone,
		asyncEventAuthError,
		asyncEventAgentTaskChanged,
		asyncEventAgentTaskCompleted:
		return true
	case asyncEventPromptDelta,
		asyncEventPromptThinkingDelta,
		asyncEventPromptToolStart,
		asyncEventPromptToolResult,
		asyncEventPromptUsage,
		asyncEventPromptUsageSnapshot:
		return false
	}

	return false
}

func asyncContextEventKind(kind assistant.StreamEventKind) asyncEventKind {
	switch kind {
	case assistant.StreamEventContextCompactionStart:
		return asyncEventCompactStart
	case assistant.StreamEventContextCompactionDone:
		return asyncEventCompactDone
	case assistant.StreamEventContextCompactionError:
		return asyncEventCompactError
	case assistant.StreamEventContextCompaction,
		assistant.StreamEventTextDelta,
		assistant.StreamEventThinkingDelta,
		assistant.StreamEventToolStart,
		assistant.StreamEventToolResult,
		assistant.StreamEventSkillLoaded,
		assistant.StreamEventUsage,
		assistant.StreamEventUsageSnapshot,
		assistant.StreamEventUnknown:
		return asyncEventPromptContext
	}

	return asyncEventPromptContext
}

func (app *App) applyPromptContextEvent(payload *asyncEvent) {
	if payload.Text != "" {
		app.addSystemMessage(payload.Text)
	}

	switch payload.Kind {
	case asyncEventCompactStart:
		app.startCompactionIndicator()
	case asyncEventCompactDone:
		app.stopCompactionIndicator()

		if payload.Text != "" {
			app.setStatus(compactedStatusMessage)
		}
	case asyncEventCompactError:
		app.stopCompactionIndicator()
	case asyncEventPromptContext:
		return
	case asyncEventAuthURL,
		asyncEventAuthDone,
		asyncEventAuthError,
		asyncEventAgentTaskChanged,
		asyncEventAgentTaskCompleted,
		asyncEventPromptDone,
		asyncEventPromptUserEntry,
		asyncEventPromptDelta,
		asyncEventPromptThinkingDelta,
		asyncEventPromptToolStart,
		asyncEventPromptToolResult,
		asyncEventPromptRetry,
		asyncEventPromptUsage,
		asyncEventPromptUsageSnapshot,
		asyncEventPromptError:
		return
	}
}

func (app *App) startCompactionIndicator() {
	app.compacting = true
	app.workStartedAt = time.Now()
	app.workFrame = 0
	app.setStatus("compacting context")
}

func (app *App) stopCompactionIndicator() {
	app.compacting = false
}

func (app *App) handlePromptStreamEvent(ctx context.Context, payload *asyncEvent) {
	switch payload.Kind {
	case asyncEventPromptDelta:
		app.emitExtensionRuntimeEventOrMessage(
			ctx,
			extensionEventModelDelta,
			map[string]any{extensionDataText: payload.Text},
		)
		app.appendStreamingBlock(transcript.RoleAssistant, payload.Text)
	case asyncEventPromptThinkingDelta:
		app.emitExtensionRuntimeEventOrMessage(
			ctx,
			extensionEventThinkingDelta,
			map[string]any{extensionDataText: payload.Text},
		)
		app.appendStreamingBlock(transcript.RoleThinking, payload.Text)
	case asyncEventPromptToolResult:
		app.emitExtensionRuntimeEventOrMessage(ctx, extensionEventToolEnd, toolExtensionData(payload.ToolEvent))
		app.applyStreamedToolEvent(payload.ToolEvent)
	case asyncEventPromptToolStart:
		app.emitExtensionRuntimeEventOrMessage(ctx, extensionEventToolStart, toolCallExtensionData(payload))
		app.applyStreamedToolStart(payload.ToolCallEvent, payload.Text)

		return
	case asyncEventPromptUsage:
		app.applyTokenUsage(payload.Usage)

		return
	case asyncEventPromptUsageSnapshot:
		app.applyTokenUsageEvent(payload.Usage, true)

		return
	case asyncEventPromptDone,
		asyncEventPromptUserEntry,
		asyncEventPromptRetry,
		asyncEventPromptError,
		asyncEventPromptContext,
		asyncEventAuthURL,
		asyncEventAuthDone,
		asyncEventAuthError,
		asyncEventAgentTaskChanged,
		asyncEventAgentTaskCompleted,
		asyncEventCompactStart,
		asyncEventCompactDone,
		asyncEventCompactError:
		return
	}
}

func (app *App) emitPromptRetryExtensionEvent(ctx context.Context, payload *asyncEvent) {
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

func toolCallExtensionData(payload *asyncEvent) map[string]any {
	if payload == nil {
		return map[string]any{}
	}

	if payload.ToolCallEvent == nil {
		return map[string]any{extensionDataName: payload.Text}
	}

	return map[string]any{
		extensionDataName:         payload.ToolCallEvent.Name,
		extensionDataToolArgsJSON: payload.ToolCallEvent.ArgumentsJSON,
		extensionDataToolCallID:   payload.ToolCallEvent.ID,
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

func (app *App) applyPromptUserEntry(_ context.Context, sessionID, entryID string, promptID uint64) {
	if app.activePrompt == nil || app.activePrompt.ID != promptID {
		return
	}

	app.activePrompt.SessionID = sessionID
	app.activePrompt.UserEntryID = entryID
}

func (app *App) applyPromptError(ctx context.Context, message string, promptID uint64) {
	streamingBlocks := append([]chatMessage(nil), app.transcript.Streaming.Blocks...)
	canceled := app.activePrompt != nil && app.activePrompt.ID == promptID && app.activePrompt.Canceled
	app.finishPrompt()

	app.applyFailedPromptStreamedBlocks(streamingBlocks)
	app.commitLiveAgentCompletions()

	if canceled {
		app.setStatus("response canceled; progress saved")
		app.processQueuedPrompt(ctx)

		return
	}

	app.addMessage(transcript.RoleCustom, message)
	app.processQueuedPrompt(ctx)
}

func (app *App) finishPrompt() {
	app.working = false
	app.streamingText = ""
	app.streamingThinkingText = ""
	app.resetStreamingBlocks()
	app.streamedToolEvents = 0
	app.activePrompt = nil
}

func (app *App) applyFailedPromptStreamedBlocks(streamingBlocks []chatMessage) {
	for _, block := range streamingBlocks {
		if block.Content == "" {
			continue
		}

		switch block.Role {
		case transcript.RoleAssistant,
			transcript.RoleToolResult,
			transcript.RoleBashExecution,
			transcript.RoleCustom:
			app.addMessage(block.Role, block.Content)
		case transcript.RoleThinking:
			if strings.TrimSpace(block.Content) != "" {
				app.addMessage(block.Role, block.Content)
			}
		case transcript.RoleUser,
			transcript.RoleBranchSummary,
			transcript.RoleCompactionSummary:
			continue
		}
	}
}

func (app *App) applyStreamedToolStart(call *assistant.ToolCallEvent, fallbackName string) {
	if call == nil {
		call = &assistant.ToolCallEvent{
			Arguments:     tool.EmptyArguments(),
			ID:            "",
			ParentCallID:  "",
			Name:          fallbackName,
			ArgumentsJSON: "",
			Sequence:      0,
		}
	}

	if strings.TrimSpace(call.Name) == "" {
		call.Name = fallbackName
	}

	if strings.TrimSpace(call.Name) == "" || isAgentManagementTool(call.Name) {
		return
	}

	app.runningToolBlocks = append(app.runningToolBlocks, runningToolBlock{Call: *call, StartedAt: time.Now()})
}

func (app *App) applyStreamedToolEvent(event *assistant.ToolEvent) {
	if event == nil {
		return
	}

	if event.Name == workflowToolName && !event.IsError {
		app.trackStartedWorkflow(context.Background(), event)
	}

	if isAgentManagementTool(event.Name) {
		app.applyAgentToolEvent(event)

		return
	}

	app.removeRunningToolBlock(event)
	app.appendStreamingBlock(transcript.RoleToolResult, formatToolEventForUI(event))
	app.streamedToolEvents++
}

func (app *App) appendStreamingBlock(role transcript.Role, content string) {
	if content == "" {
		return
	}

	lastIndex := len(app.transcript.Streaming.Blocks) - 1
	if lastIndex >= 0 &&
		transcript.CanMergeStreamingRole(role) &&
		app.transcript.Streaming.Blocks[lastIndex].Role == role {
		app.transcript.Streaming.Blocks[lastIndex].Content += content
		app.invalidateStreamingBlockCache(lastIndex)

		return
	}

	app.transcript.Streaming.Blocks = append(app.transcript.Streaming.Blocks, newChatMessage(role, content))
	if len(app.transcript.Streaming.LineCache) > 0 {
		app.transcript.Streaming.LineCache = append(
			app.transcript.Streaming.LineCache,
			emptyCachedRenderedMessage(),
		)
	}
}

func (app *App) invalidateStreamingBlockCache(index int) {
	if index >= 0 && index < len(app.transcript.Streaming.LineCache) {
		app.transcript.Streaming.LineCache[index] = emptyCachedRenderedMessage()
	}
}
