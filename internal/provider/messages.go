package provider

import (
	"strings"

	"github.com/omarluq/librecode/internal/llm"
)

func openAIResponseInput(messages []llm.Message) []any {
	input := []any{}
	for _, message := range messages {
		role, ok := openAIResponseInputRole(message.Role)
		content := messageText(message)
		if !ok || content == "" {
			continue
		}
		input = append(input, map[string]any{jsonRoleKey: role, jsonContentKey: content})
	}

	return input
}

func openAIResponseInputRole(role llm.Role) (string, bool) {
	switch role {
	case llm.RoleUser:
		return jsonUserRole, true
	case llm.RoleAssistant:
		// With store=false, Responses continuation must not rely on provider-side
		// assistant output item IDs. Replay assistant text as user-visible context.
		return jsonUserRole, true
	case llm.RoleSystem:
		return jsonUserRole, true
	case llm.RoleTool:
		return "", false
	}

	return "", false
}

func compactResponseMessages(messages []llm.Message) []llm.Message {
	compacted := make([]llm.Message, 0, len(messages))
	var pending []llm.Message
	for _, message := range messages {
		if message.Role == llm.RoleAssistant {
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

func flushPendingAssistantMessages(compacted *[]llm.Message, pending []llm.Message) {
	if len(pending) == 0 {
		return
	}
	merged := pending[len(pending)-1]
	parts := make([]string, 0, len(pending))
	for _, message := range pending {
		if text := strings.TrimSpace(messageText(message)); text != "" {
			parts = append(parts, text)
		}
	}
	merged.Content = []llm.Part{llm.TextPart(strings.TrimSpace(strings.Join(parts, "\n\n")))}
	if messageText(merged) != "" {
		*compacted = append(*compacted, merged)
	}
}

func messageText(message llm.Message) string {
	return strings.TrimSpace(partsText(message.Content))
}
