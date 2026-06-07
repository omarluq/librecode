// Package assistant orchestrates conversations, extensions, cache, and prompt execution.
package assistant

import (
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

func estimateUsageLedInputTokens(
	systemPrompt string,
	messages []database.MessageEntity,
	contributions []contextContribution,
	usageAnchor *database.ContextUsageAnchorEntity,
) int {
	if usageAnchor != nil && usageAnchor.Usage.InputTokens > 0 && usageAnchor.MessageIndex >= 0 &&
		usageAnchor.MessageIndex < len(messages) {
		trailingTokens := estimateTrailingInputTokens(messages, contributions, usageAnchor.MessageIndex+1)

		return usageAnchor.Usage.InputTokens + trailingTokens
	}

	inputTokens := estimateInputTokens(systemPrompt, messages)
	for index := range contributions {
		inputTokens += contributions[index].Tokens
	}

	return inputTokens
}

func estimateTrailingInputTokens(
	messages []database.MessageEntity,
	contributions []contextContribution,
	startIndex int,
) int {
	trailingTokens := 0
	for index := startIndex; index < len(messages); index++ {
		trailingTokens += estimateTokens(messages[index].Content)
	}
	for index := range contributions {
		trailingTokens += contributions[index].Tokens
	}

	return trailingTokens
}

func providerUsageEntity(usage model.TokenUsage) *database.EntryTokenUsageEntity {
	if usage.InputTokens <= 0 && usage.ContextTokens <= 0 && usage.ContextWindow <= 0 && usage.OutputTokens <= 0 {
		return nil
	}

	return &database.EntryTokenUsageEntity{
		ContextWindow: usage.ContextWindow,
		ContextTokens: usage.ContextTokens,
		InputTokens:   usage.InputTokens,
		OutputTokens:  usage.OutputTokens,
	}
}
