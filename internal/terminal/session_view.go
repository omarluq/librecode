package terminal

import (
	"maps"
	"slices"
	"time"

	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/tui"
)

// sessionViewState preserves presentation state while another session is being
// inspected. Prompt ownership remains on activePrompt and is intentionally not
// part of a view.
type sessionViewState struct {
	lastEscape               time.Time
	pendingParentID          *string
	scopedEnabled            map[string]bool
	promptHistoryDraft       string
	promptHistoryDraftImages []imageAttachment
	streamingThinkingText    string
	streamingText            string
	statusMessage            string
	runningToolBlocks        []runningToolBlock
	queuedMessages           []promptDraft
	liveAgentCompletions     []chatMessage
	promptHistory            []string
	promptHistoryImages      [][]imageAttachment
	hiddenQueuedMessages     []promptDraft
	scopedOrder              []string
	settings                 sessionSettingsDocument
	composerBuffer           tui.TextArea
	composerImages           []imageAttachment
	transcript               transcriptState
	tokenUsage               model.TokenUsage
	selection                mouseSelection
	transcriptList           transcriptListSelection
	streamedToolEvents       int
	promptHistoryIndex       int
	scrollOffset             int
	autocompleteSelection    int
	escapePresses            int
	autocompleteClosed       bool
}

func (app *App) saveSessionView() {
	if app.sessionID == "" {
		return
	}

	if app.sessionViews == nil {
		app.sessionViews = make(map[string]sessionViewState)
	}

	app.sessionViews[app.sessionID] = app.captureSessionView(true)
}

func (app *App) captureSessionView(clone bool) sessionViewState {
	view := sessionViewState{
		pendingParentID:          app.pendingParentID,
		transcript:               app.transcript,
		runningToolBlocks:        app.runningToolBlocks,
		liveAgentCompletions:     app.liveAgentCompletions,
		queuedMessages:           app.queuedMessages,
		hiddenQueuedMessages:     app.hiddenQueuedMessages,
		promptHistory:            app.promptHistory,
		promptHistoryImages:      app.promptHistoryImages,
		promptHistoryDraft:       app.promptHistoryDraft,
		promptHistoryDraftImages: app.promptHistoryDraftImages,
		tokenUsage:               app.tokenUsage,
		composerBuffer:           app.composerBuffer,
		composerImages:           app.composerImages,
		selection:                app.selection,
		transcriptList:           app.transcriptList,
		streamingText:            app.streamingText,
		streamingThinkingText:    app.streamingThinkingText,
		scopedEnabled:            app.scopedEnabled,
		scopedOrder:              app.scopedOrder,
		settings:                 app.currentSessionSettings(),
		streamedToolEvents:       app.streamedToolEvents,
		promptHistoryIndex:       app.promptHistoryIndex,
		scrollOffset:             app.scrollOffset,
		statusMessage:            app.statusMessage,
		autocompleteSelection:    app.autocompleteSelection,
		autocompleteClosed:       app.autocompleteClosed,
		escapePresses:            app.escapePresses,
		lastEscape:               app.lastEscape,
	}
	if clone {
		view.pendingParentID = cloneStringPtr(view.pendingParentID)
		view.tokenUsage = cloneTerminalUsage(view.tokenUsage)
		view.composerBuffer = cloneComposerBuffer(view.composerBuffer)
		view.composerImages = cloneImageAttachments(view.composerImages)
		view.promptHistoryImages = cloneImageAttachmentGroups(view.promptHistoryImages)
		view.promptHistoryDraftImages = cloneImageAttachments(view.promptHistoryDraftImages)
		view.queuedMessages = clonePromptDrafts(view.queuedMessages)
		view.hiddenQueuedMessages = clonePromptDrafts(view.hiddenQueuedMessages)
		view.scopedEnabled = maps.Clone(view.scopedEnabled)
		view.scopedOrder = slices.Clone(view.scopedOrder)
	}

	return view
}

func (app *App) restoreSessionView(sessionID string) bool {
	view, found := app.sessionViews[sessionID]
	if !found {
		return false
	}

	app.applySessionView(sessionID, &view, true)

	return true
}

func (app *App) applySessionView(sessionID string, view *sessionViewState, clone bool) {
	app.sessionID = sessionID
	app.pendingParentID = view.pendingParentID
	app.transcript = view.transcript
	app.runningToolBlocks = view.runningToolBlocks
	app.liveAgentCompletions = view.liveAgentCompletions
	app.queuedMessages = view.queuedMessages
	app.hiddenQueuedMessages = view.hiddenQueuedMessages
	app.promptHistory = view.promptHistory
	app.promptHistoryImages = view.promptHistoryImages
	app.promptHistoryDraft = view.promptHistoryDraft
	app.promptHistoryDraftImages = view.promptHistoryDraftImages
	app.tokenUsage = view.tokenUsage
	app.composerBuffer = view.composerBuffer
	app.composerImages = view.composerImages
	app.selection = view.selection
	app.transcriptList = view.transcriptList
	app.streamingText = view.streamingText
	app.streamingThinkingText = view.streamingThinkingText
	app.scopedEnabled = view.scopedEnabled
	app.scopedOrder = view.scopedOrder
	app.streamedToolEvents = view.streamedToolEvents
	app.promptHistoryIndex = view.promptHistoryIndex
	app.scrollOffset = view.scrollOffset
	app.statusMessage = view.statusMessage
	app.autocompleteSelection = view.autocompleteSelection
	app.autocompleteClosed = view.autocompleteClosed
	app.escapePresses = view.escapePresses
	app.lastEscape = view.lastEscape
	app.applySessionSettings(&view.settings)

	if clone {
		app.pendingParentID = cloneStringPtr(app.pendingParentID)
		app.tokenUsage = cloneTerminalUsage(app.tokenUsage)
		app.composerBuffer = cloneComposerBuffer(app.composerBuffer)
		app.composerImages = cloneImageAttachments(app.composerImages)
		app.promptHistoryImages = cloneImageAttachmentGroups(app.promptHistoryImages)
		app.promptHistoryDraftImages = cloneImageAttachments(app.promptHistoryDraftImages)
		app.queuedMessages = clonePromptDrafts(app.queuedMessages)
		app.hiddenQueuedMessages = clonePromptDrafts(app.hiddenQueuedMessages)
		app.scopedEnabled = maps.Clone(app.scopedEnabled)
		app.scopedOrder = slices.Clone(app.scopedOrder)
	}
}

func (app *App) inspectingWhilePromptRuns() bool {
	return app.activePrompt != nil && app.activePrompt.SessionID != "" && app.activePrompt.SessionID != app.sessionID
}

// withSessionView routes an event to its owning session without changing the
// session the user is viewing. The terminal event loop serializes these state
// transitions, so callbacks cannot observe a partially switched view. State is
// transferred rather than cloned because neither view is used concurrently.
func (app *App) withSessionView(sessionID string, apply func()) bool {
	if sessionID == "" || sessionID == app.sessionID {
		apply()

		return true
	}

	targetView, found := app.sessionViews[sessionID]
	if !found {
		return false
	}

	displayedSessionID := app.sessionID
	displayedView := app.captureSessionView(false)
	app.sessionViews[displayedSessionID] = displayedView
	app.applySessionView(sessionID, &targetView, false)

	apply()

	app.sessionViews[sessionID] = app.captureSessionView(false)
	app.applySessionView(displayedSessionID, &displayedView, false)

	return true
}

func cloneComposerBuffer(buffer tui.TextArea) tui.TextArea {
	buffer.Metadata = maps.Clone(buffer.Metadata)
	buffer.Chars = slices.Clone(buffer.Chars)

	return buffer
}
