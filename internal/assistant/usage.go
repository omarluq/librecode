package assistant

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

func estimateTokenUsage(
	systemPrompt string,
	messages []database.MessageEntity,
	selectedModel *model.Model,
) model.TokenUsage {
	inputTokens := estimateInputTokens(systemPrompt, messages)

	return model.TokenUsage{
		ContextWindow: selectedModel.ContextWindow,
		ContextTokens: inputTokens,
		InputTokens:   inputTokens,
		OutputTokens:  0,
	}
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
	if reportedTotal := reported.TotalTokens(); reportedTotal > usage.ContextTokens {
		usage.ContextTokens = reportedTotal
	}

	return usage
}

func usageFromObject(value any) model.TokenUsage {
	object, ok := value.(map[string]any)
	if !ok {
		return model.EmptyTokenUsage()
	}
	input := usageInputTokens(object)
	output := intFromAny(firstPresent(object, jsonOutputTokensKey, "completion_tokens"))

	return model.TokenUsage{ContextWindow: 0, ContextTokens: 0, InputTokens: input, OutputTokens: output}
}

func usageInputTokens(object map[string]any) int {
	input := intFromAny(firstPresent(object, "input_tokens", "prompt_tokens"))
	if input > 0 {
		return input
	}
	if total := intFromAny(object["total_tokens"]); total > 0 {
		output := intFromAny(firstPresent(object, jsonOutputTokensKey, "completion_tokens"))
		if output > 0 && total > output {
			return total - output
		}
	}

	return 0
}

func firstPresent(object map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := object[key]; ok {
			return value
		}
	}

	return nil
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		parsed, err := typed.Int64()
		if err == nil {
			return int(parsed)
		}
	}

	return 0
}
