package provider

import (
	"time"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

const (
	testBranchContent       = "branch"
	testCompactionContent   = "compaction"
	testCustomContent       = "custom"
	testThinkingDelta       = "thinking"
	testOpenAIProvider      = "openai"
	testProviderMessageType = jsonMessageType
	testToolArgumentsJSON   = `{"path":"README.md"}`
)

func providerTestMessageEntity(role database.Role, content string) database.MessageEntity {
	return database.MessageEntity{Timestamp: time.Time{}, Role: role, Content: content, Provider: "", Model: ""}
}

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
