package llm_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/tool"
)

func TestUsageRoundTripClonesMutableFields(t *testing.T) {
	t.Parallel()

	const changedLabel = "mutated"

	usage := model.TokenUsage{
		Breakdown: map[string]int{"system": 10},
		TopContributors: []model.TokenContributor{{
			Label:   "entry",
			Role:    "user",
			Preview: "hello",
			Tokens:  7,
			Chars:   28,
		}},
		ContextWindow: 100,
		ContextTokens: 50,
		InputTokens:   40,
		OutputTokens:  10,
	}

	converted := llm.UsageFromModel(usage)
	usage.Breakdown["system"] = 99
	usage.TopContributors[0].Label = changedLabel

	assert.Equal(t, 10, converted.Breakdown["system"])
	require.Len(t, converted.TopContributors, 1)
	assert.Equal(t, "entry", converted.TopContributors[0].Label)

	roundTripped := llm.UsageToModel(converted)
	converted.Breakdown["system"] = 33
	converted.TopContributors[0].Label = "changed"

	assert.Equal(t, 10, roundTripped.Breakdown["system"])
	require.Len(t, roundTripped.TopContributors, 1)
	assert.Equal(t, "entry", roundTripped.TopContributors[0].Label)
	assert.Equal(t, 100, roundTripped.ContextWindow)
	assert.Equal(t, 50, roundTripped.ContextTokens)
	assert.Equal(t, 40, roundTripped.InputTokens)
	assert.Equal(t, 10, roundTripped.OutputTokens)
}

func TestModelRefFromModelClonesMetadata(t *testing.T) {
	t.Parallel()

	input := model.Model{
		ThinkingLevelMap: nil,
		Headers:          nil,
		Compat:           map[string]any{"reasoning": "adaptive"},
		Provider:         "anthropic",
		ID:               "claude-opus",
		Name:             "Claude Opus",
		API:              "anthropic-messages",
		BaseURL:          "https://api.anthropic.com",
		Input:            nil,
		Cost:             model.Cost{Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0},
		ContextWindow:    1_000_000,
		MaxTokens:        32_000,
		Reasoning:        true,
	}

	ref := llm.ModelRefFromModel(&input)
	input.Compat["reasoning"] = "mutated"

	assert.Equal(t, "anthropic", ref.Provider)
	assert.Equal(t, "claude-opus", ref.ID)
	assert.Equal(t, "anthropic-messages", ref.API)
	assert.Equal(t, "https://api.anthropic.com", ref.BaseURL)
	assert.Equal(t, 1_000_000, ref.ContextWindow)
	assert.Equal(t, 32_000, ref.MaxTokens)
	assert.True(t, ref.Reasoning)
	assert.Equal(t, "adaptive", ref.Metadata["reasoning"])
}

func TestAuthFromModelClonesHeaders(t *testing.T) {
	t.Parallel()

	auth := model.RequestAuth{
		Headers: map[string]string{"x-test": "before"},
		APIKey:  "secret",
		Error:   "",
		OK:      true,
	}

	converted := llm.AuthFromModel(auth)
	auth.Headers["x-test"] = "after"

	assert.Equal(t, "secret", converted.APIKey)
	assert.Equal(t, "before", converted.Headers["x-test"])
}

func TestToolDefinitionFromToolClonesSchema(t *testing.T) {
	t.Parallel()

	definition := tool.Definition{
		Schema:           map[string]any{"type": "object"},
		Name:             tool.NameRead,
		Label:            "Read",
		Description:      "Read a file",
		PromptSnippet:    "",
		PromptGuidelines: nil,
		ReadOnly:         true,
	}

	converted := llm.ToolDefinitionFromTool(&definition)
	definition.Schema["type"] = "mutated"

	assert.Equal(t, "read", converted.Name)
	assert.Equal(t, "Read a file", converted.Description)
	assert.True(t, converted.ReadOnly)
	assert.Equal(t, "object", converted.Schema["type"])
}

func TestProviderErrorHelpers(t *testing.T) {
	t.Parallel()

	cause := assert.AnError
	err := &llm.ProviderError{
		Cause:        cause,
		Metadata:     nil,
		Kind:         llm.ErrorKindContextOverflow,
		Provider:     "openai",
		Model:        "gpt",
		Code:         "context_window_exceeded",
		ProviderCode: "too_large",
		Message:      "context window exceeded",
		StatusCode:   400,
	}

	providerError, ok := llm.AsProviderError(err)
	require.True(t, ok)
	assert.Same(t, err, providerError)
	assert.True(t, llm.IsKind(err, llm.ErrorKindContextOverflow))
	assert.False(t, llm.IsKind(err, llm.ErrorKindRateLimit))
	assert.Equal(t, cause, err.Unwrap())
	assert.Equal(t, "context window exceeded", err.Error())
}

func TestTextHelpers(t *testing.T) {
	t.Parallel()

	message := llm.TextMessage(llm.RoleUser, "hello")
	require.Len(t, message.Content, 1)
	assert.Equal(t, llm.RoleUser, message.Role)
	assert.Equal(t, llm.PartText, message.Content[0].Type)
	assert.Equal(t, "hello", message.Content[0].Text)
}
