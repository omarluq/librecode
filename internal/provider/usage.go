package provider

import (
	"encoding/json"

	"github.com/omarluq/librecode/internal/llm"
)

func mergeUsage(estimated, reported llm.Usage) llm.Usage {
	usage := estimated
	if reported.ContextWindow > 0 {
		usage.ContextWindow = reported.ContextWindow
	}

	if reported.ContextTokens > 0 {
		usage.ContextTokens = reported.ContextTokens
	}

	if reported.InputTokens > 0 {
		usage.InputTokens = reported.InputTokens
	}

	if reported.OutputTokens > 0 {
		usage.OutputTokens = reported.OutputTokens
	}

	return llm.MergeUsage(usage, reported)
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
