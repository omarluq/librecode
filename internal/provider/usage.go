package provider

import (
	"encoding/json"
	"math"

	"github.com/omarluq/librecode/internal/llm"
)

const jsonTotalTokensKey = "total_tokens"

func mergeUsage(estimated, reported llm.Usage) llm.Usage {
	return llm.MergeUsage(estimated, reported)
}

func accumulateUsage(aggregate, reported llm.Usage) llm.Usage {
	usage := llm.MergeUsage(aggregate, reported)
	usage.InputTokens = checkedTokenAdd(aggregate.InputTokens, reported.InputTokens)
	usage.OutputTokens = checkedTokenAdd(aggregate.OutputTokens, reported.OutputTokens)

	if reported.ContextTokens <= 0 && reported.InputTokens > 0 {
		usage.ContextTokens = reported.InputTokens
	}

	return usage
}

func usageFromObject(value any) llm.Usage {
	object, ok := value.(map[string]any)
	if !ok {
		return llm.EmptyUsage()
	}

	input := usageInputTokens(object)
	output := intFromAny(firstPresent(object, jsonOutputTokensKey, jsonCompletionTokensKey))
	reported := hasAnyKey(object,
		jsonInputTokensKey, jsonPromptTokensKey, jsonOutputTokensKey, jsonCompletionTokensKey,
		"cache_read_input_tokens", "cache_creation_input_tokens", jsonTotalTokensKey,
	)

	usage := llm.Usage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   0,
		ContextTokens:   0,
		InputTokens:     input,
		OutputTokens:    output,
	}
	if reported {
		usage = usage.WithReported()
	}

	return usage
}

func usageInputTokens(object map[string]any) int {
	input := intFromAny(firstPresent(object, jsonInputTokensKey, jsonPromptTokensKey))
	cacheInput := checkedTokenAdd(
		intFromAny(object["cache_read_input_tokens"]),
		intFromAny(object["cache_creation_input_tokens"]),
	)

	if input > 0 || cacheInput > 0 {
		return checkedTokenAdd(input, cacheInput)
	}

	if total := intFromAny(object[jsonTotalTokensKey]); total > 0 {
		output := intFromAny(firstPresent(object, jsonOutputTokensKey, jsonCompletionTokensKey))
		if output > 0 && total > output {
			return total - output
		}
	}

	return 0
}

func hasAnyKey(object map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, ok := object[key]; ok {
			return true
		}
	}

	return false
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
		return nonnegativeInt(typed)
	case int64:
		return intFromInt64(typed)
	case float64:
		return intFromFloat64(typed)
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return 0
		}

		return intFromInt64(parsed)
	default:
		return 0
	}
}

func nonnegativeInt(value int) int {
	if value < 0 {
		return 0
	}

	return value
}

func intFromInt64(value int64) int {
	if value < 0 || uint64(value) > uint64(math.MaxInt) {
		return 0
	}

	return int(value)
}

func intFromFloat64(value float64) int {
	if value < 0 || value > float64(math.MaxInt) || math.Trunc(value) != value {
		return 0
	}

	converted := uint64(value)
	if converted > uint64(math.MaxInt) {
		return 0
	}

	return int(converted)
}

func checkedTokenAdd(left, right int) int {
	if left < 0 || right < 0 {
		return 0
	}

	if left > math.MaxInt-right {
		return math.MaxInt
	}

	return left + right
}
