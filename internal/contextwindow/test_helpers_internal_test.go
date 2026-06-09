package contextwindow

import "github.com/omarluq/librecode/internal/model"

func testModelWithContextWindow(contextWindow int) *model.Model {
	return &model.Model{
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
		ContextWindow:    contextWindow,
		MaxTokens:        0,
		Reasoning:        false,
	}
}
