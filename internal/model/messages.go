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
	converted := cloneMessage(message)
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
