package terminal

import (
	"context"
	"strings"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
)

func (app *App) applyPromptResponse(ctx context.Context, response *assistant.PromptResponse, promptID uint64) {
	if app.consumeCanceledPrompt(promptID) {
		return
	}
	if app.activePrompt != nil && app.activePrompt.Canceled {
		app.activePrompt = nil
		return
	}
	streamingBlocks := append([]chatMessage(nil), app.transcript.Streaming.Blocks...)
	app.working = false
	app.streamingText = ""
	app.streamingThinkingText = ""
	app.resetStreamingBlocks()
	if response == nil {
		app.activePrompt = nil
		app.streamedToolEvents = 0
		app.processQueuedPrompt(ctx)
		return
	}
	app.sessionID = response.SessionID
	app.applyTokenUsage(&response.Usage)
	app.applyPromptResponseSideEffects(response, streamingBlocks)
	app.streamedToolEvents = 0
	app.addMessage(database.RoleAssistant, response.Text)
	app.activePrompt = nil
	app.persistSessionSettings()
	app.processQueuedPrompt(ctx)
}

func (app *App) applyPromptResponseSideEffects(response *assistant.PromptResponse, streamingBlocks []chatMessage) {
	streamedThinkingBlocks := 0
	streamedToolBlocks := app.streamedToolEvents
	if len(streamingBlocks) > 0 {
		streamedThinkingBlocks, streamedToolBlocks = app.applyStreamedSideEffectBlocks(streamingBlocks)
	}
	app.applyRemainingSideEffects(response, streamedThinkingBlocks, streamedToolBlocks)
}

func (app *App) applyStreamedSideEffectBlocks(
	streamingBlocks []chatMessage,
) (streamedThinkingBlocks, streamedToolBlocks int) {
	streamedThinkingBlocks = 0
	streamedToolBlocks = 0
	for _, block := range streamingBlocks {
		thinkingBlock, toolBlock := app.applyStreamedSideEffectBlock(block)
		streamedThinkingBlocks += thinkingBlock
		streamedToolBlocks += toolBlock
	}

	return streamedThinkingBlocks, streamedToolBlocks
}

func (app *App) applyStreamedSideEffectBlock(block chatMessage) (thinkingBlocks, toolBlocks int) {
	switch block.Role {
	case database.RoleThinking:
		return app.applyStreamedThinkingBlock(block.Content), 0
	case database.RoleToolResult, database.RoleBashExecution:
		app.addMessage(block.Role, block.Content)
		return 0, 1
	case database.RoleAssistant,
		database.RoleUser,
		database.RoleCustom,
		database.RoleBranchSummary,
		database.RoleCompactionSummary:
		return 0, 0
	}

	return 0, 0
}

func (app *App) applyStreamedThinkingBlock(content string) int {
	if strings.TrimSpace(content) == "" {
		return 0
	}
	app.addMessage(database.RoleThinking, content)

	return 1
}

func (app *App) applyRemainingSideEffects(
	response *assistant.PromptResponse,
	streamedThinkingBlocks int,
	streamedToolBlocks int,
) {
	for index := streamedThinkingBlocks; index < len(response.Thinking); index++ {
		app.addMessage(database.RoleThinking, response.Thinking[index])
	}
	for index := streamedToolBlocks; index < len(response.ToolEvents); index++ {
		app.addMessage(database.RoleToolResult, formatToolEventForUI(&response.ToolEvents[index]))
	}
}
