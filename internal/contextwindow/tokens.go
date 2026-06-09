package contextwindow

import (
	"strings"
	"unicode/utf8"

	"github.com/omarluq/librecode/internal/database"
)

// EstimateTokens returns a rough cross-provider estimate used until provider usage arrives.
func EstimateTokens(text string) int {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0
	}
	runes := utf8.RuneCountInString(trimmed)
	if runes == 0 {
		return 0
	}

	return max(1, (runes+3)/4)
}

// EstimateInputTokens estimates the model-facing input tokens for system prompt plus messages.
func EstimateInputTokens(systemPrompt string, messages []database.MessageEntity) int {
	count := EstimateTokens(systemPrompt)
	for index := range messages {
		count += EstimateTokens(messages[index].Content)
	}

	return count
}

// EstimateMessageTokens estimates the model-facing token count for messages.
func EstimateMessageTokens(messages []database.MessageEntity) int {
	tokens := 0
	for index := range messages {
		tokens += EstimateTokens(messages[index].Content)
	}

	return tokens
}
