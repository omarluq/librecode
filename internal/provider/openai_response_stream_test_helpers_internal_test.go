package provider

import (
	"bytes"
	"encoding/json"
	"strings"
)

func openAIResponseCompletedStream(responseJSON string) string {
	var compact bytes.Buffer

	body := strings.TrimSpace(responseJSON)
	if err := json.Compact(&compact, []byte(body)); err != nil {
		panic("openAIResponseCompletedStream: failed to compact test response: " + err.Error())
	}

	return "data: {\"type\":\"response.completed\",\"response\":" + compact.String() + "}\n\n"
}

func responseFunctionCallJSON(callID, name, arguments string) string {
	payload, err := json.Marshal(map[string]any{
		jsonOutputKey: []any{map[string]any{
			jsonTypeKey:      functionCallType,
			jsonCallIDKey:    callID,
			jsonToolNameKey:  name,
			jsonArgumentsKey: arguments,
		}},
	})
	if err != nil {
		panic("responseFunctionCallJSON: failed to marshal test payload: " + err.Error())
	}

	return string(payload)
}
