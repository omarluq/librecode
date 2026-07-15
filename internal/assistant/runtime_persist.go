// Package assistant orchestrates conversations, extensions, cache, and prompt execution.
package assistant

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/transcript"
)

const (
	promptPersistenceTimeout  = 5 * time.Second
	promptCanceledMessage     = "[system] response canceled by user"
	toolCallCanceledMessage   = "tool call canceled by user"
	toolCallIncompleteMessage = "tool call did not complete"
)

type partialPromptBlock struct {
	Role    transcript.Role
	Content string
}

type pendingToolCall struct {
	Name          string
	ArgumentsJSON string
}

type partialPromptMessage struct {
	Role    database.Role
	Content string
}

type partialPromptProgress struct {
	forward        func(StreamEvent)
	blocks         []partialPromptBlock
	fallbackBlocks []partialPromptBlock
	pendingTools   []pendingToolCall
}

func (runtime *Runtime) appendAssistantSideEffects(
	ctx context.Context,
	sessionID string,
	parentID *string,
	bundle *responseBundle,
) (*string, error) {
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

	for index := range bundle.ToolEvents {
		event := &bundle.ToolEvents[index]
		message := database.MessageEntity{
			Timestamp: time.Now().UTC(),
			Role:      database.RoleToolResult,
			Content:   formatToolEvent(event),
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
		userEntryID,
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
	return &partialPromptProgress{
		forward:        forward,
		blocks:         []partialPromptBlock{},
		fallbackBlocks: nil,
		pendingTools:   []pendingToolCall{},
	}
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
		progress.append(transcript.RoleAssistant, streamEvent.Text)
	case StreamEventThinkingDelta:
		progress.append(transcript.RoleThinking, streamEvent.Text)
	case StreamEventToolResult:
		if streamEvent.ToolEvent != nil {
			progress.removePendingTool(streamEvent.ToolEvent)
			progress.append(transcript.RoleToolResult, formatToolEvent(streamEvent.ToolEvent))
		}
	case StreamEventToolStart:
		progress.trackPendingTool(streamEvent.ToolCallEvent, streamEvent.Text)
	case StreamEventSkillLoaded,
		StreamEventUsage,
		StreamEventUsageSnapshot,
		StreamEventContextCompaction,
		StreamEventContextCompactionStart,
		StreamEventContextCompactionDone,
		StreamEventContextCompactionError,
		StreamEventUnknown:
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
	progress.pendingTools = progress.pendingTools[:0]
}

func (progress *partialPromptProgress) append(role transcript.Role, content string) {
	if progress == nil || content == "" {
		return
	}

	lastIndex := len(progress.blocks) - 1
	if lastIndex >= 0 && progress.blocks[lastIndex].Role == role && transcript.CanMergeStreamingRole(role) {
		progress.blocks[lastIndex].Content += content

		return
	}

	progress.blocks = append(progress.blocks, partialPromptBlock{Role: role, Content: content})
}

func promptPersistenceContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), promptPersistenceTimeout)
}

func (runtime *Runtime) appendPartialPromptFailure(
	ctx context.Context,
	sessionID string,
	userEntryID string,
	progress *partialPromptProgress,
	promptErr error,
) error {
	persistCtx, cancel := promptPersistenceContext(ctx)
	defer cancel()

	_, err := runtime.appendPartialPromptMessages(
		persistCtx,
		sessionID,
		&userEntryID,
		progress.failureMessages(promptErr),
	)

	return err
}

func (runtime *Runtime) appendPartialPromptMessages(
	ctx context.Context,
	sessionID string,
	parentID *string,
	messages []partialPromptMessage,
) (*string, error) {
	for _, partial := range messages {
		message := database.MessageEntity{
			Timestamp: time.Now().UTC(),
			Role:      partial.Role,
			Content:   partial.Content,
			Provider:  runtime.cfg.Assistant.Provider,
			Model:     runtime.cfg.Assistant.Model,
		}

		entry, err := runtime.sessions.AppendMessage(ctx, sessionID, parentID, &message)
		if err != nil {
			return nil, oops.In("assistant").Code("append_partial_prompt").Wrapf(err, "append partial prompt progress")
		}

		runtime.dispatchMessageAppend(ctx, entry)
		parentID = &entry.ID
	}

	return parentID, nil
}

func (progress *partialPromptProgress) failureMessages(promptErr error) []partialPromptMessage {
	blocks := progress.persistableBlocks()
	toolEvents := progress.syntheticToolFailureEvents(promptErr)
	messages := make([]partialPromptMessage, 0, len(blocks)+len(toolEvents)+1)

	for _, block := range blocks {
		messages = append(messages, partialPromptMessage{
			Role:    transcript.ToDatabaseRole(block.Role),
			Content: block.Content,
		})
	}

	for index := range toolEvents {
		messages = append(messages, partialPromptMessage{
			Role:    database.RoleToolResult,
			Content: formatToolEvent(&toolEvents[index]),
		})
	}

	messages = append(messages, partialPromptMessage{
		Role:    database.RoleCustom,
		Content: partialPromptErrorMessage(promptErr),
	})

	return messages
}

func (progress *partialPromptProgress) trackPendingTool(call *ToolCallEvent, fallbackName string) {
	if progress == nil {
		return
	}

	pending := pendingToolCall{Name: fallbackName, ArgumentsJSON: ""}
	if call != nil {
		pending.Name = call.Name
		pending.ArgumentsJSON = call.ArgumentsJSON
	}

	if pending.Name == "" {
		pending.Name = fallbackName
	}

	if pending.Name == "" {
		return
	}

	progress.pendingTools = append(progress.pendingTools, pending)
}

func (progress *partialPromptProgress) removePendingTool(event *ToolEvent) {
	if progress == nil || event == nil || len(progress.pendingTools) == 0 {
		return
	}

	index, found := progress.pendingToolIndex(event.Name, event.ArgumentsJSON)
	if !found {
		return
	}

	progress.pendingTools = slices.Delete(progress.pendingTools, index, index+1)
}

func (progress *partialPromptProgress) pendingToolIndex(name, argumentsJSON string) (int, bool) {
	for index, pending := range progress.pendingTools {
		if pending.Name == name && pending.ArgumentsJSON == argumentsJSON {
			return index, true
		}
	}

	for index, pending := range progress.pendingTools {
		if pending.Name == name {
			return index, true
		}
	}

	return 0, false
}

func (progress *partialPromptProgress) syntheticToolFailureEvents(promptErr error) []ToolEvent {
	if progress == nil || len(progress.pendingTools) == 0 {
		return nil
	}

	message := syntheticToolFailureMessage(promptErr)

	events := make([]ToolEvent, 0, len(progress.pendingTools))
	for _, pending := range progress.pendingTools {
		events = append(events, ToolEvent{
			CallID:       "",
			ParentCallID: "",
			Sequence:     0,

			Name:          pending.Name,
			ArgumentsJSON: pending.ArgumentsJSON,
			DetailsJSON:   "",
			Result:        message,
			Error:         message,
			IsError:       true,
		})
	}

	return events
}

func syntheticToolFailureMessage(promptErr error) string {
	if errors.Is(promptErr, context.Canceled) {
		return toolCallCanceledMessage
	}

	return toolCallIncompleteMessage
}

func partialPromptErrorMessage(promptErr error) string {
	if errors.Is(promptErr, context.Canceled) {
		return promptCanceledMessage
	}

	return "[system] " + promptErr.Error()
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

	return slices.Clone(blocks)
}

func formatToolEvent(toolEvent *ToolEvent) string {
	if toolEvent == nil {
		return transcript.FormatToolEventPersistence(nil)
	}

	return transcript.FormatToolEventPersistence(&transcript.ToolEvent{
		Name:          toolEvent.Name,
		ArgumentsJSON: toolEvent.ArgumentsJSON,
		DetailsJSON:   toolEvent.DetailsJSON,
		Result:        toolEvent.Result,
		Error:         toolEvent.Error,
		IsError:       toolEvent.IsError,
	})
}
