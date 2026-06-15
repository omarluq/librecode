package provider

import (
	"encoding/json"
	"strings"
)

const openAIChatDoneLine = "data: " + sseDoneData

func openAIChatChunk(payload map[string]any) string {
	encoded, err := json.Marshal(payload)
	if err != nil {
		panic("openAIChatChunk: failed to marshal test payload: " + err.Error())
	}

	return "data: " + string(encoded)
}

func openAIChatStream(lines ...string) string {
	stream := make([]string, 0, len(lines)*2)
	for _, line := range lines {
		stream = append(stream, line, "")
	}

	return strings.Join(stream, "\n")
}

func openAIChatDelta(delta map[string]any, finishReason string, usage map[string]any) string {
	choice := map[string]any{anthropicDeltaKey: delta}
	if finishReason != "" {
		choice[jsonFinishReasonKey] = finishReason
	}

	if len(usage) > 0 {
		choice[jsonUsageKey] = usage
	}

	return openAIChatChunk(map[string]any{jsonChoicesKey: []any{choice}})
}
