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
	lastEscape            time.Time
	pendingParentID       *string
	scopedEnabled         map[string]bool
	promptHistoryDraft    string
	streamingThinkingText string
	streamingText         string
	statusMessage         string
	runningToolBlocks     []runningToolBlock
	queuedMessages        []string
	liveAgentCompletions  []chatMessage
	promptHistory         []string
	hiddenQueuedMessages  []string
	scopedOrder           []string
	settings              sessionSettingsDocument
	composerBuffer        tui.TextArea
	transcript            transcriptState
	tokenUsage            model.TokenUsage
	selection             mouseSelection
	transcriptList        transcriptListSelection
	streamedToolEvents    int
	promptHistoryIndex    int
	scrollOffset          int
	autocompleteSelection int
	escapePresses         int
	autocompleteClosed    bool
}

func (app *App) saveSessionView() {
	if app.sessionID == "" {
		return
	}

	if app.sessionViews == nil {
		app.sessionViews = make(map[string]sessionViewState)
	}

	app.sessionViews[app.sessionID] = sessionViewState{
		pendingParentID:       cloneStringPtr(app.pendingParentID),
		transcript:            app.transcript,
		runningToolBlocks:     app.runningToolBlocks,
		liveAgentCompletions:  app.liveAgentCompletions,
		queuedMessages:        app.queuedMessages,
		hiddenQueuedMessages:  app.hiddenQueuedMessages,
		promptHistory:         app.promptHistory,
		promptHistoryDraft:    app.promptHistoryDraft,
		tokenUsage:            cloneTerminalUsage(app.tokenUsage),
		composerBuffer:        cloneComposerBuffer(app.composerBuffer),
		selection:             app.selection,
		transcriptList:        app.transcriptList,
		streamingText:         app.streamingText,
		streamingThinkingText: app.streamingThinkingText,
		scopedEnabled:         maps.Clone(app.scopedEnabled),
		scopedOrder:           slices.Clone(app.scopedOrder),
		settings:              app.currentSessionSettings(),
		streamedToolEvents:    app.streamedToolEvents,
		promptHistoryIndex:    app.promptHistoryIndex,
		scrollOffset:          app.scrollOffset,
		statusMessage:         app.statusMessage,
		autocompleteSelection: app.autocompleteSelection,
		autocompleteClosed:    app.autocompleteClosed,
		escapePresses:         app.escapePresses,
		lastEscape:            app.lastEscape,
	}
}

func (app *App) restoreSessionView(sessionID string) bool {
	view, found := app.sessionViews[sessionID]
	if !found {
		return false
	}

	app.sessionID = sessionID
	app.pendingParentID = cloneStringPtr(view.pendingParentID)
	app.transcript = view.transcript
	app.runningToolBlocks = view.runningToolBlocks
	app.liveAgentCompletions = view.liveAgentCompletions
	app.queuedMessages = view.queuedMessages
	app.hiddenQueuedMessages = view.hiddenQueuedMessages
	app.promptHistory = view.promptHistory
	app.promptHistoryDraft = view.promptHistoryDraft
	app.tokenUsage = cloneTerminalUsage(view.tokenUsage)
	app.composerBuffer = cloneComposerBuffer(view.composerBuffer)
	app.selection = view.selection
	app.transcriptList = view.transcriptList
	app.streamingText = view.streamingText
	app.streamingThinkingText = view.streamingThinkingText
	app.scopedEnabled = maps.Clone(view.scopedEnabled)
	app.scopedOrder = slices.Clone(view.scopedOrder)
	app.streamedToolEvents = view.streamedToolEvents
	app.promptHistoryIndex = view.promptHistoryIndex
	app.scrollOffset = view.scrollOffset
	app.statusMessage = view.statusMessage
	app.autocompleteSelection = view.autocompleteSelection
	app.autocompleteClosed = view.autocompleteClosed
	app.escapePresses = view.escapePresses
	app.lastEscape = view.lastEscape
	app.applySessionSettings(&view.settings)

	return true
}

// withSessionView routes an event to its owning session without changing the
// session the user is viewing. The terminal event loop serializes these state
// transitions, so callbacks cannot observe a partially switched view.
func (app *App) inspectingWhilePromptRuns() bool {
	return app.activePrompt != nil && app.activePrompt.SessionID != "" && app.activePrompt.SessionID != app.sessionID
}

func (app *App) withSessionView(sessionID string, apply func()) bool {
	if sessionID == "" || sessionID == app.sessionID {
		apply()

		return true
	}

	displayedSessionID := app.sessionID
	app.saveSessionView()

	if !app.restoreSessionView(sessionID) {
		app.restoreSessionView(displayedSessionID)

		return false
	}

	apply()
	app.saveSessionView()
	app.restoreSessionView(displayedSessionID)

	return true
}

func cloneComposerBuffer(buffer tui.TextArea) tui.TextArea {
	buffer.Metadata = maps.Clone(buffer.Metadata)
	buffer.Chars = slices.Clone(buffer.Chars)

	return buffer
}
