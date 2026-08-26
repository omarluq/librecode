package model

import (
	"strings"

	"github.com/omarluq/librecode/internal/database"
)

const (
	// CompactionSummaryPrefix wraps compacted conversation history.
	CompactionSummaryPrefix = "The conversation history before this point was compacted into the following summary:" +
		"\n\n<summary>\n"
	// CompactionSummarySuffix closes a compacted summary block.
	CompactionSummarySuffix = "\n</summary>"
	// BranchSummaryPrefix wraps summaries from abandoned branches.
	BranchSummaryPrefix = "The following is a summary of a branch that this conversation came back from:\n\n<summary>\n"
	// BranchSummarySuffix closes a branch summary block.
	BranchSummarySuffix = "</summary>"
)

// IsFacingRole reports whether a persisted message role is replayed to models.
func IsFacingRole(role database.Role) bool {
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

// FacingMessage converts persisted summary roles into model-facing user messages.
func FacingMessage(message *database.MessageEntity) database.MessageEntity {
	converted := cloneMessage(message)
	switch message.Role {
	case database.RoleCompactionSummary:
		converted.Role = database.RoleUser
		converted.Content = CompactionSummaryPrefix + message.Content + CompactionSummarySuffix
	case database.RoleBranchSummary:
		converted.Role = database.RoleUser
		converted.Content = BranchSummaryPrefix + message.Content + BranchSummarySuffix
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

// IsFacingMessage reports whether a persisted message has model-facing content.
func IsFacingMessage(message *database.MessageEntity) bool {
	if message == nil || !IsFacingRole(message.Role) {
		return false
	}

	if strings.TrimSpace(message.Content) != "" {
		return true
	}

	for index := range message.Parts {
		part := &message.Parts[index]
		if part.Type == database.MessagePartImage && len(part.Data) > 0 {
			return true
		}
	}

	return false
}

func cloneMessage(message *database.MessageEntity) database.MessageEntity {
	converted := *message
	if message.Parts == nil {
		return converted
	}

	converted.Parts = make([]database.MessagePartEntity, len(message.Parts))
	copy(converted.Parts, message.Parts)

	for index := range converted.Parts {
		converted.Parts[index].Data = append([]byte(nil), message.Parts[index].Data...)
	}

	return converted
}
