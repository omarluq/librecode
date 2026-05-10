package assistant

import (
	"strings"

	"github.com/omarluq/librecode/internal/database"
)

func openAIResponseInput(messages []database.MessageEntity) []any {
	input := []any{}
	for _, message := range messages {
		role, ok := openAIResponseInputRole(message.Role)
		if !ok || message.Content == "" {
			continue
		}
		input = append(input, map[string]any{jsonRoleKey: role, jsonContentKey: message.Content})
	}

	return input
}

func openAIResponseInputRole(role database.Role) (string, bool) {
	switch role {
	case database.RoleUser:
		return jsonUserRole, true
	case database.RoleAssistant:
		// With store=false, Responses continuation must not rely on provider-side
		// assistant output item IDs. Replay assistant text as user-visible context.
		return jsonUserRole, true
	case database.RoleToolResult,
		database.RoleThinking,
		database.RoleCustom,
		database.RoleBashExecution,
		database.RoleBranchSummary,
		database.RoleCompactionSummary:
		return "", false
	}

	return "", false
}

func compactResponseMessages(messages []database.MessageEntity) []database.MessageEntity {
	compacted := make([]database.MessageEntity, 0, len(messages))
	var pending []database.MessageEntity
	for _, message := range messages {
		if message.Role == database.RoleAssistant {
			pending = append(pending, message)
			continue
		}
		flushPendingAssistantMessages(&compacted, pending)
		pending = nil
		compacted = append(compacted, message)
	}
	flushPendingAssistantMessages(&compacted, pending)

	return compacted
}

func flushPendingAssistantMessages(compacted *[]database.MessageEntity, pending []database.MessageEntity) {
	if len(pending) == 0 {
		return
	}
	merged := pending[len(pending)-1]
	parts := make([]string, 0, len(pending))
	for _, message := range pending {
		if text := strings.TrimSpace(message.Content); text != "" {
			parts = append(parts, text)
		}
	}
	merged.Content = strings.TrimSpace(strings.Join(parts, "\n\n"))
	if merged.Content != "" {
		*compacted = append(*compacted, merged)
	}
}
