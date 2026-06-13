// Package assistant orchestrates conversations, extensions, cache, and prompt execution.
package assistant

import (
	"context"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

func (runtime *Runtime) modelContextEntity(
	ctx context.Context,
	sessionID string,
) (*database.SessionContextEntity, error) {
	leafEntry, _, err := runtime.sessions.LeafEntry(ctx, sessionID)
	if err != nil {
		return nil, oops.In("assistant").Code("load_context_leaf").Wrapf(err, "load session leaf")
	}

	leafID := ""
	if leafEntry != nil {
		leafID = leafEntry.ID
	}

	contextEntity, err := runtime.sessions.BuildContext(ctx, sessionID, leafID)
	if err != nil {
		return nil, oops.In("assistant").Code("load_context").Wrapf(err, "load session context")
	}

	return contextEntity, nil
}

func modelFacingMessages(messages []database.MessageEntity) []database.MessageEntity {
	return model.FacingMessages(messages)
}

func remapUsageAnchor(
	anchor *database.ContextUsageAnchorEntity,
	originalMessages []database.MessageEntity,
	modelMessages []database.MessageEntity,
) *database.ContextUsageAnchorEntity {
	if anchor == nil || anchor.MessageIndex < 0 || anchor.MessageIndex >= len(originalMessages) {
		return nil
	}

	anchorMessage := originalMessages[anchor.MessageIndex]
	modelIndex := -1

	for originalIndex := range originalMessages[:anchor.MessageIndex+1] {
		message := originalMessages[originalIndex]
		if model.IsFacingMessage(&message) {
			modelIndex++
		}
	}

	if modelIndex < 0 || modelIndex >= len(modelMessages) {
		return nil
	}

	if modelMessages[modelIndex].Timestamp != anchorMessage.Timestamp {
		return nil
	}

	remapped := *anchor
	remapped.MessageIndex = modelIndex

	return &remapped
}

func baseSystemPrompt(cwd string) string {
	return strings.Join([]string{
		"You are librecode, an AI coding assistant. Be concise, helpful, and accurate.",
		"You are running inside a local filesystem workspace.",
		"Current working directory: " + cwd,
		"Use built-in tools (ls, find, grep, read, bash, edit, write) " +
			"to inspect or change workspace files when needed.",
		"Do not claim you cannot access files; inspect them with tools instead.",
		"Respect .gitignore and default ignored paths; avoid ignored files unless explicitly needed.",
		"Use the fewest tool calls needed; once you have enough evidence, stop using tools and answer.",
	}, "\n")
}
