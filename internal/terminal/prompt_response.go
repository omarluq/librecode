package terminal

import (
	"context"
	"strings"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/transcript"
)

func (app *App) applyPromptResponse(ctx context.Context, response *assistant.PromptResponse, promptID uint64) {
	streamingBlocks := append([]chatMessage(nil), app.transcript.Streaming.Blocks...)
	if app.activePrompt == nil || app.activePrompt.ID != promptID {
		return
	}

	if app.activePrompt.Canceled {
		app.finishPrompt()
		app.applyFailedPromptStreamedBlocks(streamingBlocks)

		if response != nil {
			app.sessionID = response.SessionID
			app.applyTokenUsage(&response.Usage)
		}

		app.setStatus("response canceled; progress saved")
		app.processQueuedPrompt(ctx)

		return
	}

	app.finishPrompt()

	if response == nil {
		app.processQueuedPrompt(ctx)

		return
	}

	app.sessionID = response.SessionID
	app.applyTokenUsage(&response.Usage)
	app.applyPromptResponseSideEffects(response, streamingBlocks)
	app.addMessage(transcript.RoleAssistant, response.Text)
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
	case transcript.RoleThinking:
		return app.applyStreamedThinkingBlock(block.Content), 0
	case transcript.RoleToolResult, transcript.RoleBashExecution:
		app.addMessage(block.Role, block.Content)

		return 0, 1
	case transcript.RoleAssistant,
		transcript.RoleUser,
		transcript.RoleCustom,
		transcript.RoleBranchSummary,
		transcript.RoleCompactionSummary:
		return 0, 0
	}

	return 0, 0
}

func (app *App) applyStreamedThinkingBlock(content string) int {
	if strings.TrimSpace(content) == "" {
		return 0
	}

	app.addMessage(transcript.RoleThinking, content)

	return 1
}

func (app *App) applyRemainingSideEffects(
	response *assistant.PromptResponse,
	streamedThinkingBlocks int,
	streamedToolBlocks int,
) {
	for index := streamedThinkingBlocks; index < len(response.Thinking); index++ {
		app.addMessage(transcript.RoleThinking, response.Thinking[index])
	}

	for index := streamedToolBlocks; index < len(response.ToolEvents); index++ {
		app.addMessage(transcript.RoleToolResult, formatToolEventForUI(&response.ToolEvents[index]))
	}
}
