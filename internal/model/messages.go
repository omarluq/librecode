package model

import (
	"strings"

	"github.com/omarluq/librecode/internal/core"
	"github.com/omarluq/librecode/internal/database"
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

// IsFacingMessage reports whether a persisted message has model-facing content.
func IsFacingMessage(message *database.MessageEntity) bool {
	return message != nil && IsFacingRole(message.Role) && strings.TrimSpace(message.Content) != ""
}
