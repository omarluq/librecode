package llm

// EmptyRequest returns an explicit zero-value provider-neutral request.
func EmptyRequest() Request {
	return Request{
		ProviderOptions: nil,
		Auth:            Auth{Headers: nil, APIKey: ""},
		SystemPrompt:    "",
		ThinkingLevel:   "",
		SessionID:       "",
		Messages:        nil,
		Tools:           nil,
		Model: ModelRef{
			Metadata:         nil,
			ThinkingLevelMap: nil,
			Provider:         "",
			ID:               "",
			API:              "",
			BaseURL:          "",
			MaxTokens:        0,
			ContextWindow:    0,
			Reasoning:        false,
		},
		Usage:        EmptyUsage(),
		DisableTools: false,
	}
}
