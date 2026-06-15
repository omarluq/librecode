package provider

func addZAIChatPayloadOptions(payload map[string]any, request *CompletionRequest, hasTools bool) {
	if request == nil || request.Request.Model.Provider != "zai" {
		return
	}

	if request.Request.Model.Reasoning {
		thinkingType := thinkingDisabled
		if request.Request.ThinkingLevel != "" && request.Request.ThinkingLevel != thinkingOff {
			thinkingType = thinkingEnabled
		}

		payload[jsonThinkingKey] = map[string]any{jsonTypeKey: thinkingType}
	}

	if hasTools {
		payload["tool_stream"] = true
	}
}
