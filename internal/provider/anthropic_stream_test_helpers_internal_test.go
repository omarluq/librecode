package provider

import (
	"encoding/json"
	"strings"
)

const (
	anthropicEventMessageStart      = "event: message_start"
	anthropicEventMessageDelta      = "event: message_delta"
	anthropicEventMessageStop       = "event: message_stop"
	anthropicEventContentBlockDelta = "event: content_block_delta"
	anthropicMessageStopData        = `data: {"type":"message_stop"}`
)

func anthropicResponseJSON(stopReason, text string, stopDetails *anthropicStopDetails) string {
	content := []map[string]any{}
	if text != "" {
		content = append(content, map[string]any{jsonTypeKey: jsonTextKey, jsonTextKey: text})
	}

	payload := map[string]any{
		anthropicStopReasonKey: stopReason,
		jsonContentKey:         content,
	}
	if stopDetails != nil {
		payload["stop_details"] = stopDetails
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		panic("anthropicResponseJSON: failed to marshal test payload: " + err.Error())
	}

	return string(encoded)
}

func anthropicResponseStream(responseJSON string) string {
	var response struct {
		Usage       map[string]any        `json:"usage"`
		StopDetails *anthropicStopDetails `json:"stop_details"`
		StopReason  string                `json:"stop_reason"`
		Content     []struct {
			Input any    `json:"input"`
			Type  string `json:"type"`
			Text  string `json:"text"`
			ID    string `json:"id"`
			Name  string `json:"name"`
		} `json:"content"`
	}
	if err := json.Unmarshal([]byte(responseJSON), &response); err != nil {
		panic("anthropicResponseStream: failed to unmarshal test response: " + err.Error())
	}

	lines := []string{}

	if len(response.Usage) > 0 {
		message, err := json.Marshal(map[string]any{
			jsonTypeKey:    anthropicMessageStartEvent,
			jsonMessageKey: map[string]any{jsonUsageKey: response.Usage},
		})
		if err != nil {
			panic("anthropicResponseStream: failed to marshal test usage event: " + err.Error())
		}

		lines = append(lines, anthropicEventMessageStart, "data: "+string(message), "")
	}

	for index, block := range response.Content {
		switch block.Type {
		case jsonTextKey:
			lines = append(lines,
				anthropicEventContentBlockDelta,
				"data: "+anthropicContentDeltaJSON(index, anthropicTextDelta, jsonTextKey, block.Text),
				"",
			)
		case anthropicToolUseType:
			lines = append(
				lines,
				"event: content_block_start",
				anthropicToolUseBlockData(index, block.ID, block.Name, block.Input),
				"",
			)
		}
	}

	lines = append(lines, anthropicMessageDeltaLines(response.StopReason, response.StopDetails)...)

	return strings.Join(lines, "\n")
}

func anthropicToolUseBlockData(index int, callID, name string, input any) string {
	payload, err := json.Marshal(map[string]any{
		jsonTypeKey:  anthropicContentBlockStartEvent,
		jsonIndexKey: index,
		"content_block": map[string]any{
			jsonTypeKey:     anthropicToolUseType,
			"id":            callID,
			jsonToolNameKey: name,
			jsonInputKey:    input,
		},
	})
	if err != nil {
		panic("anthropicToolUseBlockData: failed to marshal test payload: " + err.Error())
	}

	return "data: " + string(payload)
}

func anthropicMessageDeltaLines(stopReason string, stopDetails *anthropicStopDetails) []string {
	if stopReason == "" {
		stopReason = "end_turn"
	}

	delta := map[string]any{anthropicStopReasonKey: stopReason}
	if stopDetails != nil {
		delta["stop_details"] = stopDetails
	}

	payload, err := json.Marshal(map[string]any{
		jsonTypeKey:       anthropicMessageDeltaEvent,
		anthropicDeltaKey: delta,
	})
	if err != nil {
		panic("anthropicMessageDeltaLines: failed to marshal test payload: " + err.Error())
	}

	return []string{
		anthropicEventMessageDelta,
		"data: " + string(payload),
		"",
		anthropicEventMessageStop,
		anthropicMessageStopData,
		"",
	}
}

func anthropicContentDeltaJSON(index int, deltaType, field, value string) string {
	event := map[string]any{
		jsonTypeKey:  anthropicContentBlockDeltaEvent,
		jsonIndexKey: index,
		anthropicDeltaKey: map[string]any{
			jsonTypeKey: deltaType,
			field:       value,
		},
	}

	encoded, err := json.Marshal(event)
	if err != nil {
		panic("anthropicContentDeltaJSON: failed to marshal test payload: " + err.Error())
	}

	return string(encoded)
}
