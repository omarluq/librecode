// Package assistant orchestrates conversations, extensions, cache, and prompt execution.
package assistant

import (
	"context"
	"fmt"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/core"
	"github.com/omarluq/librecode/internal/database"
)

func (runtime *Runtime) modelContextMessages(ctx context.Context, sessionID string) ([]database.MessageEntity, error) {
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

	return modelFacingMessages(contextEntity.Messages), nil
}

func modelFacingMessages(messages []database.MessageEntity) []database.MessageEntity {
	filtered := make([]database.MessageEntity, 0, len(messages))
	for index := range messages {
		message := messages[index]
		if !isModelFacingRole(message.Role) || strings.TrimSpace(message.Content) == "" {
			continue
		}
		filtered = append(filtered, modelFacingMessage(&message))
	}

	return filtered
}

func modelFacingMessage(message *database.MessageEntity) database.MessageEntity {
	converted := *message
	switch message.Role {
	case database.RoleCompactionSummary:
		converted.Role = database.RoleUser
		converted.Content = core.CompactionSummaryPrefix + message.Content + core.CompactionSummarySuffix
	case database.RoleBranchSummary:
		converted.Role = database.RoleUser
		converted.Content = core.BranchSummaryPrefix + message.Content + core.BranchSummarySuffix
	case database.RoleUser,
		database.RoleAssistant,
		database.RoleToolResult,
		database.RoleThinking,
		database.RoleCustom,
		database.RoleBashExecution:
		return converted
	}

	return converted
}

func isModelFacingRole(role database.Role) bool {
	switch role {
	case database.RoleUser,
		database.RoleAssistant,
		database.RoleBranchSummary,
		database.RoleCompactionSummary,
		database.RoleCustom,
		database.RoleBashExecution:
		return true
	case database.RoleToolResult,
		database.RoleThinking:
		return false
	}

	return false
}

func baseSystemPrompt(cwd string) string {
	return strings.Join([]string{
		"You are librecode, an AI coding assistant. Be concise, helpful, and accurate.",
		"You are running inside a local filesystem workspace.",
		fmt.Sprintf("Current working directory: %s", cwd),
		"Use built-in tools (ls, find, grep, read, bash, edit, write) " +
			"to inspect or change workspace files when needed.",
		"Do not claim you cannot access files; inspect them with tools instead.",
		"Respect .gitignore and default ignored paths; avoid ignored files unless explicitly needed.",
		"Use the fewest tool calls needed; once you have enough evidence, stop using tools and answer.",
	}, "\n")
}
