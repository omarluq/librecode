package model_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	assert.Contains(t, model.DefaultModelPerProvider, "anthropic")
	assert.Contains(t, model.DefaultModelPerProvider, "anthropic-claude")
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
		"opencode",
		"opencode-go",
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
