package provider

import (
	"encoding/json"

	"github.com/omarluq/librecode/internal/llm"
)

func mergeUsage(estimated, reported llm.Usage) llm.Usage {
	return llm.MergeUsage(estimated, reported)
}

func accumulateUsage(aggregate, reported llm.Usage) llm.Usage {
	usage := llm.MergeUsage(aggregate, reported)
	usage.InputTokens = aggregate.InputTokens + reported.InputTokens
	usage.OutputTokens = aggregate.OutputTokens + reported.OutputTokens

	return usage
}

func usageFromObject(value any) llm.Usage {
	object, ok := value.(map[string]any)
	if !ok {
		return llm.EmptyUsage()
	}

	input := usageInputTokens(object)
	output := intFromAny(firstPresent(object, jsonOutputTokensKey, "completion_tokens"))

	return llm.Usage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   0,
		ContextTokens:   0,
		InputTokens:     input,
		OutputTokens:    output,
	}
}

func usageInputTokens(object map[string]any) int {
	input := intFromAny(firstPresent(object, jsonInputTokensKey, "prompt_tokens"))
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
