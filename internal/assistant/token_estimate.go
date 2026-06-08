package assistant

import (
	"maps"
	"strings"
	"unicode/utf8"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

func mergeUsage(estimated, reported model.TokenUsage) model.TokenUsage {
	usage := estimated
	if reported.ContextWindow > 0 {
		usage.ContextWindow = reported.ContextWindow
	}
	if reported.ContextTokens > usage.ContextTokens {
		usage.ContextTokens = reported.ContextTokens
	}
	if reported.InputTokens > 0 {
		usage.InputTokens = reported.InputTokens
	}
	if reported.OutputTokens > 0 {
		usage.OutputTokens = reported.OutputTokens
	}
	if len(usage.Breakdown) == 0 && len(reported.Breakdown) > 0 {
		usage.Breakdown = cloneIntMapForMerge(reported.Breakdown)
	}
	if len(usage.TopContributors) == 0 && len(reported.TopContributors) > 0 {
		usage.TopContributors = cloneTokenContributors(reported.TopContributors)
	}

	return usage
}

func cloneIntMapForMerge(input map[string]int) map[string]int {
	if input == nil {
		return nil
	}
	output := make(map[string]int, len(input))
	maps.Copy(output, input)

	return output
}

func cloneTokenContributors(contributors []model.TokenContributor) []model.TokenContributor {
	if len(contributors) == 0 {
		return nil
	}
	cloned := make([]model.TokenContributor, len(contributors))
	copy(cloned, contributors)

	return cloned
}

func estimateInputTokens(systemPrompt string, messages []database.MessageEntity) int {
	count := estimateTokens(systemPrompt)
	for index := range messages {
		count += estimateTokens(messages[index].Content)
	}

	return count
}

func estimateTokens(text string) int {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0
	}
	runes := utf8.RuneCountInString(trimmed)
	if runes == 0 {
		return 0
	}

	// Rough cross-provider estimate used until provider usage arrives.
	return max(1, (runes+3)/4)
}
