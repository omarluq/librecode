package assistant

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/contextwindow"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/tool"
)

func TestLLMRequestFromCompletionRequestConvertsAssistantState(t *testing.T) {
	t.Parallel()

	registry := tool.NewRegistry(t.TempDir())
	request := &CompletionRequest{
		OnEvent:           nil,
		OnProviderObserve: nil,
		OnProviderRequest: nil,
		ToolRegistry:      registry,
		ExecuteTools:      nil,
		SessionID:         "session-1",
		SystemPrompt:      jsonSystemRole,
		ThinkingLevel:     "high",
		CWD:               t.TempDir(),
		Auth: model.RequestAuth{
			Headers: map[string]string{"x-test": "value"},
			APIKey:  "secret",
			Error:   "",
			OK:      true,
		},
		Messages: []database.MessageEntity{
			testMessageEntity(database.RoleUser, "hello"),
			testMessageEntity(database.RoleThinking, "private reasoning"),
			testMessageEntity(database.RoleAssistant, "answer"),
			testMessageEntity(database.RoleToolResult, "tool output"),
			testMessageEntity(database.RoleUser, "   "),
		},
		Usage: model.EmptyTokenUsage(),
		Model: model.Model{
			ThinkingLevelMap: nil,
			Headers:          nil,
			Compat:           map[string]any{"compat": "yes"},
			Provider:         "openai",
			ID:               "gpt-test",
			Name:             "Test",
			API:              apiOpenAIResponses,
			BaseURL:          "https://example.test",
			Input:            nil,
			Cost:             model.Cost{Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0},
			ContextWindow:    128_000,
			MaxTokens:        4096,
			Reasoning:        true,
		},
		ProviderAttempt: 0,
		DisableTools:    false,
	}

	converted := llmRequestFromCompletionRequest(request)

	assert.Equal(t, "session-1", converted.SessionID)
	assert.Equal(t, jsonSystemRole, converted.SystemPrompt)
	assert.Equal(t, "high", converted.ThinkingLevel)
	assert.Equal(t, "openai", converted.Model.Provider)
	assert.Equal(t, "gpt-test", converted.Model.ID)
	assert.Equal(t, "secret", converted.Auth.APIKey)
	assert.Equal(t, "value", converted.Auth.Headers["x-test"])
	assert.Len(t, converted.Messages, 4)
	assert.Equal(t, llm.RoleUser, converted.Messages[0].Role)
	assert.Equal(t, llm.RoleAssistant, converted.Messages[1].Role)
	assert.Equal(t, llm.RoleAssistant, converted.Messages[2].Role)
	assert.Equal(t, llm.RoleTool, converted.Messages[3].Role)
	require.NotEmpty(t, converted.Tools)

	converted.Auth.Headers["x-test"] = testLLMMutatedLabel
	assert.Equal(t, "value", request.Auth.Headers["x-test"])
	converted.Model.Metadata["compat"] = testLLMMutatedLabel
	assert.Equal(t, "yes", request.Model.Compat["compat"])
}

func TestLLMRequestFromCompletionRequestNilAndDisabledTools(t *testing.T) {
	t.Parallel()

	empty := llmRequestFromCompletionRequest(nil)
	assert.Empty(t, empty.Messages)
	assert.Empty(t, empty.Tools)

	converted := llmRequestFromCompletionRequest(&CompletionRequest{
		OnEvent:           nil,
		OnProviderObserve: nil,
		OnProviderRequest: nil,
		ToolRegistry:      tool.NewRegistry(t.TempDir()),
		ExecuteTools:      nil,
		SessionID:         "",
		SystemPrompt:      "",
		ThinkingLevel:     "",
		CWD:               "",
		Auth:              model.RequestAuth{Headers: nil, APIKey: "", Error: "", OK: false},
		Messages:          nil,
		Usage:             model.EmptyTokenUsage(),
		Model:             emptyTestModel(),
		ProviderAttempt:   0,
		DisableTools:      true,
	})
	assert.Empty(t, converted.Tools)
	assert.True(t, converted.DisableTools)
}

func TestLLMResponseFromCompletionResultConvertsContentAndUsage(t *testing.T) {
	t.Parallel()

	result := &CompletionResult{
		Text:     "final answer",
		Thinking: []string{" thought ", "   "},
		ToolEvents: []ToolEvent{{
			Name:          jsonReadToolName,
			ArgumentsJSON: `{"path":"README.md"}`,
			DetailsJSON:   "",
			Result:        "read output",
			Error:         "",
			IsError:       false,
		}, {
			Name:          "bash",
			ArgumentsJSON: `{"command":"false"}`,
			DetailsJSON:   "",
			Result:        "exit status 1",
			Error:         "exit status 1",
			IsError:       true,
		}},
		Usage: model.TokenUsage{
			Breakdown:       map[string]int{contextwindow.BreakdownHistory: 10},
			TopContributors: nil,
			ContextWindow:   100,
			ContextTokens:   20,
			InputTokens:     18,
			OutputTokens:    2,
		},
	}

	converted := llmResponseFromCompletionResult(result)

	assert.Equal(t, llm.FinishReasonStop, converted.FinishReason)
	assert.Equal(t, 18, converted.Usage.InputTokens)
	require.Len(t, converted.Content, 4)
	assert.Equal(t, llm.PartReasoning, converted.Content[0].Type)
	assert.Equal(t, "thought", converted.Content[0].Text)
	assert.Equal(t, llm.PartText, converted.Content[1].Type)
	assert.Equal(t, "final answer", converted.Content[1].Text)
	assert.Equal(t, llm.PartToolResult, converted.Content[2].Type)
	assert.False(t, converted.Content[2].ToolResult.IsError)
	assert.Equal(t, llm.PartToolResult, converted.Content[3].Type)
	assert.True(t, converted.Content[3].ToolResult.IsError)
	assert.Equal(t, "exit status 1", converted.Content[3].ToolResult.Error)
}

func TestLLMUsageToModelRoundTrips(t *testing.T) {
	t.Parallel()

	usage := llm.Usage{
		Breakdown:       map[string]int{"system": 1},
		TopContributors: nil,
		ContextWindow:   10,
		ContextTokens:   2,
		InputTokens:     2,
		OutputTokens:    1,
	}

	converted := llmUsageToModel(usage)

	assert.Equal(t, 10, converted.ContextWindow)
	assert.Equal(t, 2, converted.InputTokens)
	assert.Equal(t, map[string]int{"system": 1}, converted.Breakdown)
}

func emptyTestModel() model.Model {
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
