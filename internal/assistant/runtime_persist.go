// Package assistant orchestrates conversations, extensions, cache, and prompt execution.
package assistant

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/database"
)

type partialPromptBlock struct {
	Role    database.Role
	Content string
}

type partialPromptProgress struct {
	forward        func(StreamEvent)
	blocks         []partialPromptBlock
	fallbackBlocks []partialPromptBlock
}

func (runtime *Runtime) appendAssistantSideEffects(
	ctx context.Context,
	sessionID string,
	userEntryID string,
	bundle *responseBundle,
) (*string, error) {
	parentID := &userEntryID
	for _, thinking := range bundle.Thinking {
		trimmed := strings.TrimSpace(thinking)
		if trimmed == "" {
			continue
		}
		message := database.MessageEntity{
			Timestamp: time.Now().UTC(),
			Role:      database.RoleThinking,
			Content:   trimmed,
			Provider:  runtime.cfg.Assistant.Provider,
			Model:     runtime.cfg.Assistant.Model,
		}
		entry, err := runtime.sessions.AppendMessage(ctx, sessionID, parentID, &message)
		if err != nil {
			return nil, oops.In("assistant").Code("append_thinking").Wrapf(err, "append thinking message")
		}
		runtime.dispatchMessageAppend(ctx, entry)
		parentID = &entry.ID
	}
	for _, event := range bundle.ToolEvents {
		message := database.MessageEntity{
			Timestamp: time.Now().UTC(),
			Role:      database.RoleToolResult,
			Content:   formatToolEvent(&event),
			Provider:  runtime.cfg.Assistant.Provider,
			Model:     runtime.cfg.Assistant.Model,
		}
		entry, err := runtime.sessions.AppendMessage(ctx, sessionID, parentID, &message)
		if err != nil {
			return nil, oops.In("assistant").Code("append_tool_result").Wrapf(err, "append tool result")
		}
		runtime.dispatchMessageAppend(ctx, entry)
		parentID = &entry.ID
	}

	return parentID, nil
}

func (runtime *Runtime) respondWithPartialProgress(
	ctx context.Context,
	sessionID string,
	userEntryID string,
	request *PromptRequest,
) (*responseBundle, bool, error) {
	progress := newPartialPromptProgress(request.OnEvent)
	bundle, cached, err := runtime.respond(
		ctx,
		sessionID,
		request.CWD,
		request.Text,
		progress.handle,
		progress.retryHandler(request.OnRetry),
	)
	if err != nil {
		persistErr := runtime.appendPartialPromptFailure(ctx, sessionID, userEntryID, progress, err)
		if persistErr != nil {
			return nil, false, oops.
				In("assistant").
				Code("persist_failed_prompt").
				Wrapf(persistErr, "persist failed prompt progress")
		}

		return nil, false, err
	}

	return bundle, cached, nil
}

func newPartialPromptProgress(forward func(StreamEvent)) *partialPromptProgress {
	return &partialPromptProgress{forward: forward, blocks: []partialPromptBlock{}, fallbackBlocks: nil}
}

func (progress *partialPromptProgress) handle(streamEvent StreamEvent) {
	if progress != nil {
		progress.record(streamEvent)
	}
	if progress != nil && progress.forward != nil {
		progress.forward(streamEvent)
	}
}

func (progress *partialPromptProgress) record(streamEvent StreamEvent) {
	switch streamEvent.Kind {
	case StreamEventTextDelta:
		progress.append(database.RoleAssistant, streamEvent.Text)
	case StreamEventThinkingDelta:
		progress.append(database.RoleThinking, streamEvent.Text)
	case StreamEventToolResult:
		if streamEvent.ToolEvent != nil {
			progress.append(database.RoleToolResult, formatToolEvent(streamEvent.ToolEvent))
		}
	case StreamEventToolStart,
		StreamEventSkillLoaded,
		StreamEventUsage:
		return
	}
}

func (progress *partialPromptProgress) retryHandler(forward RetryEventHandler) RetryEventHandler {
	return func(retryEvent RetryEvent) {
		if retryEvent.Kind == RetryEventStart {
			progress.reset()
		}
		if forward != nil {
			forward(retryEvent)
		}
	}
}

func (progress *partialPromptProgress) reset() {
	if progress == nil {
		return
	}
	if len(progress.blocks) > 0 {
		progress.fallbackBlocks = progressBlocks(progress.blocks)
	}
	progress.blocks = progress.blocks[:0]
}

func (progress *partialPromptProgress) append(role database.Role, content string) {
	if progress == nil || content == "" {
		return
	}
	lastIndex := len(progress.blocks) - 1
	if lastIndex >= 0 && progress.blocks[lastIndex].Role == role && canMergePartialPromptBlock(role) {
		progress.blocks[lastIndex].Content += content
		return
	}
	progress.blocks = append(progress.blocks, partialPromptBlock{Role: role, Content: content})
}

func canMergePartialPromptBlock(role database.Role) bool {
	return role == database.RoleAssistant || role == database.RoleThinking
}

func (runtime *Runtime) appendPartialPromptFailure(
	ctx context.Context,
	sessionID string,
	userEntryID string,
	progress *partialPromptProgress,
	promptErr error,
) error {
	parentID := &userEntryID
	for _, block := range progress.persistableBlocks() {
		message := database.MessageEntity{
			Timestamp: time.Now().UTC(),
			Role:      block.Role,
			Content:   block.Content,
			Provider:  runtime.cfg.Assistant.Provider,
			Model:     runtime.cfg.Assistant.Model,
		}
		entry, err := runtime.sessions.AppendMessage(ctx, sessionID, parentID, &message)
		if err != nil {
			return oops.In("assistant").Code("append_partial_prompt").Wrapf(err, "append partial prompt progress")
		}
		runtime.dispatchMessageAppend(ctx, entry)
		parentID = &entry.ID
	}
	message := database.MessageEntity{
		Timestamp: time.Now().UTC(),
		Role:      database.RoleCustom,
		Content:   "[system] " + promptErr.Error(),
		Provider:  runtime.cfg.Assistant.Provider,
		Model:     runtime.cfg.Assistant.Model,
	}
	entry, err := runtime.sessions.AppendMessage(ctx, sessionID, parentID, &message)
	if err != nil {
		return oops.In("assistant").Code("append_prompt_error").Wrapf(err, "append prompt error")
	}
	runtime.dispatchMessageAppend(ctx, entry)

	return nil
}

func (progress *partialPromptProgress) persistableBlocks() []partialPromptBlock {
	if progress == nil {
		return nil
	}
	if len(progress.blocks) > 0 {
		return progressBlocks(progress.blocks)
	}
	return progressBlocks(progress.fallbackBlocks)
}

func progressBlocks(blocks []partialPromptBlock) []partialPromptBlock {
	if len(blocks) == 0 {
		return nil
	}
	clone := make([]partialPromptBlock, len(blocks))
	copy(clone, blocks)

	return clone
}

func formatToolEvent(toolEvent *ToolEvent) string {
	parts := []string{fmt.Sprintf("tool: %s", toolEvent.Name)}
	if strings.TrimSpace(toolEvent.ArgumentsJSON) != "" {
		parts = append(parts, "arguments:", toolEvent.ArgumentsJSON)
	}
	if toolEvent.Error != "" {
		parts = append(parts, "error:", toolEvent.Error)
	}
	if strings.TrimSpace(toolEvent.DetailsJSON) != "" {
		parts = append(parts, "details:", toolEvent.DetailsJSON)
	}
	if strings.TrimSpace(toolEvent.Result) != "" {
		parts = append(parts, "output:", toolEvent.Result)
	}

	return strings.Join(parts, "\n")
}
