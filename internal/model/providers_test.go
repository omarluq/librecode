package model_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/anthropicmodel"
	"github.com/omarluq/librecode/internal/model"
)

func TestBuiltInProvidersAreSupportedAPIFamilies(t *testing.T) {
	t.Parallel()

	supportedAPIs := map[string]bool{
		"anthropic-messages":     true,
		"openai-codex-responses": true,
		"openai-completions":     true,
		"openai-responses":       true,
	}

	builtIns := model.BuiltInModels()
	require.NotEmpty(t, builtIns)

	for _, builtIn := range builtIns {
		t.Run(builtIn.Provider, func(t *testing.T) {
			t.Parallel()

			if builtIn.Provider != "azure-openai-responses" {
				assert.NotEmpty(t, builtIn.BaseURL)
			}
			assert.True(t, supportedAPIs[builtIn.API], "unsupported api %q", builtIn.API)
			assert.Contains(t, model.ProviderDisplayNames, builtIn.Provider)
			assert.Contains(t, model.DefaultModelPerProvider, builtIn.Provider)
		})
	}
}

func TestAnthropicAPIAndSubscriptionProvidersAreSeparate(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Anthropic API", model.ProviderDisplayNames["anthropic"])
	assert.Equal(t, "Claude Pro/Max (Anthropic OAuth)", model.ProviderDisplayNames["anthropic-claude"])
	assert.Equal(t, anthropicmodel.Fable5, model.DefaultModelPerProvider["anthropic"])
	assert.Equal(t, anthropicmodel.Fable5, model.DefaultModelPerProvider["anthropic-claude"])
}

func TestAnthropicBuiltInDefaultsSupportFableAndMythos(t *testing.T) {
	t.Parallel()

	for _, provider := range []string{"anthropic", "anthropic-claude"} {
		for _, modelID := range []string{anthropicmodel.Fable5, anthropicmodel.Mythos5} {
			t.Run(provider+"/"+modelID, func(t *testing.T) {
				t.Parallel()

				builtIn := findBuiltIn(t, provider, modelID)
				assert.Equal(t, 1_000_000, builtIn.ContextWindow)
				assert.Equal(t, 128_000, builtIn.MaxTokens)
				assert.True(t, builtIn.Reasoning)
				assert.NotNil(t, builtIn.ThinkingLevelMap)
				assert.Contains(t, builtIn.ThinkingLevelMap, model.ThinkingOff)
				assert.Contains(t, builtIn.ThinkingLevelMap, model.ThinkingXHigh)
			})
		}
	}
}

func findBuiltIn(t *testing.T, provider, modelID string) model.Model {
	t.Helper()

	builtIns := model.BuiltInModels()
	for index := range builtIns {
		builtIn := builtIns[index]
		if builtIn.Provider == provider && builtIn.ID == modelID {
			return builtIn
		}
	}
	require.Failf(t, "built-in model not found", "%s/%s", provider, modelID)

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

func TestBuiltInProviderCatalogIsTrimmedToImplementedProviders(t *testing.T) {
	t.Parallel()

	unsupportedProviders := []string{
		"amazon-bedrock",
		"cloudflare-ai-gateway",
		"cloudflare-workers-ai",
		"fireworks",
		"github-copilot",
		"google",
		"google-vertex",
		"huggingface",
		"kimi-coding",
		"minimax",
		"minimax-cn",
		"xiaomi",
		"xiaomi-token-plan-ams",
		"xiaomi-token-plan-cn",
		"xiaomi-token-plan-sgp",
	}

	for _, provider := range unsupportedProviders {
		t.Run(provider, func(t *testing.T) {
			t.Parallel()

			assert.NotContains(t, model.ProviderDisplayNames, provider)
			assert.NotContains(t, model.DefaultModelPerProvider, provider)
		})
	}
}
