package provider

import "github.com/omarluq/librecode/internal/model"

func emptyRequestAuth() model.RequestAuth {
	return model.RequestAuth{Headers: nil, APIKey: "", Error: "", OK: false}
}

func emptyModel() model.Model {
	return model.Model{
		ThinkingLevelMap: nil,
		Headers:          nil,
		Compat:           nil,
		Provider:         "",
		ID:               "",
		Name:             "",
		API:              "",
		BaseURL:          "",
		Input:            nil,
		Cost:             model.Cost{Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0},
		ContextWindow:    0,
		MaxTokens:        0,
		Reasoning:        false,
	}
}
