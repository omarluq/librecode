package model_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/anthropicmodel"
	"github.com/omarluq/librecode/internal/model"
)

const (
	testOpenAIProvider     = "openai"
	testOpenAIBaseURL      = "https://api.openai.com/v1"
	testOpenAIResponsesAPI = "openai-responses"
	testGPT56Sol           = "gpt-5.6-sol"
)

func TestBuiltInProvidersAreSupportedAPIFamilies(t *testing.T) {
	t.Parallel()

	supportedAPIs := map[string]bool{
		"anthropic-messages":     true,
		"openai-codex-responses": true,
		"openai-completions":     true,
		testOpenAIResponsesAPI:   true,
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
			assert.Contains(t, model.ProviderDisplayNames(), builtIn.Provider)
			assert.Contains(t, model.DefaultModelPerProvider(), builtIn.Provider)
		})
	}
}

func TestAnthropicAPIAndSubscriptionProvidersAreSeparate(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Anthropic API", model.ProviderDisplayNames()["anthropic"])
	assert.Equal(t, "Claude Pro/Max (Anthropic OAuth)", model.ProviderDisplayNames()["anthropic-claude"])
	assert.Equal(t, anthropicmodel.Fable5, model.DefaultModelPerProvider()["anthropic"])
	assert.Equal(t, anthropicmodel.Fable5, model.DefaultModelPerProvider()["anthropic-claude"])
}

func TestOpenAIBuiltInDefaultsUseGPT56Sol(t *testing.T) {
	t.Parallel()

	assert.Equal(t, testGPT56Sol, model.DefaultModelPerProvider()[testOpenAIProvider])
	assert.Equal(t, testGPT56Sol, model.DefaultModelPerProvider()["openai-codex"])

	for _, testCase := range gpt56BuiltInTests() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builtIn := findBuiltIn(t, testCase.provider, testCase.modelID)
			assertGPT56BuiltIn(t, &builtIn, &testCase)
		})
	}
}

type gpt56BuiltInTest struct {
	name              string
	provider          string
	api               string
	baseURL           string
	modelID           string
	contextWindow     int
	cost              model.Cost
	requireMinimalLow bool
}

func gpt56BuiltInTests() []gpt56BuiltInTest {
	return []gpt56BuiltInTest{
		{
			name: "openai responses sol", provider: testOpenAIProvider,
			api: testOpenAIResponsesAPI, baseURL: testOpenAIBaseURL, modelID: testGPT56Sol,
			contextWindow: 272_000, cost: model.Cost{Input: 5, Output: 30, CacheRead: 0.5, CacheWrite: 6.25},
			requireMinimalLow: false,
		},
		{
			name: "openai responses terra", provider: testOpenAIProvider,
			api: testOpenAIResponsesAPI, baseURL: testOpenAIBaseURL, modelID: "gpt-5.6-terra",
			contextWindow: 272_000, cost: model.Cost{Input: 2.5, Output: 15, CacheRead: 0.25, CacheWrite: 3.125},
			requireMinimalLow: false,
		},
		{
			name: "openai responses luna", provider: testOpenAIProvider,
			api: testOpenAIResponsesAPI, baseURL: testOpenAIBaseURL, modelID: "gpt-5.6-luna",
			contextWindow: 272_000, cost: model.Cost{Input: 1, Output: 6, CacheRead: 0.1, CacheWrite: 1.25},
			requireMinimalLow: false,
		},
		{
			name: "azure openai responses sol", provider: "azure-openai-responses",
			api: testOpenAIResponsesAPI, baseURL: "", modelID: testGPT56Sol,
			contextWindow: 1_050_000, cost: model.Cost{Input: 5, Output: 30, CacheRead: 0.5, CacheWrite: 6.25},
			requireMinimalLow: false,
		},
		{
			name: "codex responses sol", provider: "openai-codex",
			api: "openai-codex-responses", baseURL: "https://chatgpt.com/backend-api", modelID: testGPT56Sol,
			contextWindow: 372_000, cost: model.Cost{Input: 5, Output: 30, CacheRead: 0.5, CacheWrite: 6.25},
			requireMinimalLow: true,
		},
	}
}

func assertGPT56BuiltIn(t *testing.T, builtIn *model.Model, test *gpt56BuiltInTest) {
	t.Helper()

	assert.Equal(t, test.api, builtIn.API)
	assert.Equal(t, test.baseURL, builtIn.BaseURL)
	assert.Equal(t, test.contextWindow, builtIn.ContextWindow)
	assert.Equal(t, 128_000, builtIn.MaxTokens)
	assert.Equal(t, test.cost, builtIn.Cost)
	assert.True(t, builtIn.Reasoning)
	assertThinkingLevelMapping(t, builtIn, model.ThinkingXHigh, "xhigh")
	assertThinkingLevelMapping(t, builtIn, model.ThinkingMax, "max")

	if test.requireMinimalLow {
		assertThinkingLevelMapping(t, builtIn, model.ThinkingMinimal, "low")
	}
}

func assertThinkingLevelMapping(t *testing.T, builtIn *model.Model, level model.ThinkingLevel, expected string) {
	t.Helper()

	mapped := builtIn.ThinkingLevelMap[level]
	require.NotNil(t, mapped)
	assert.Equal(t, expected, *mapped)
}

func TestZAIBuiltInDefaultUsesGLM52Metadata(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "glm-5.2", model.DefaultModelPerProvider()["zai"])

	builtIn := findBuiltIn(t, "zai", "glm-5.2")
	assert.Equal(t, "https://api.z.ai/api/coding/paas/v4", builtIn.BaseURL)
	assert.Equal(t, 1_000_000, builtIn.ContextWindow)
	assert.Equal(t, 131_072, builtIn.MaxTokens)
	assert.True(t, builtIn.Reasoning)
	assert.Equal(t, []model.InputMode{model.InputText}, builtIn.Input)

	assertThinkingLevelMapping(t, &builtIn, model.ThinkingHigh, "high")
	assertThinkingLevelMapping(t, &builtIn, model.ThinkingXHigh, "max")
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

	require.FailNowf(t, "built-in model not found", "%s/%s", provider, modelID)

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

			assert.NotContains(t, model.ProviderDisplayNames(), provider)
			assert.NotContains(t, model.DefaultModelPerProvider(), provider)
		})
	}
}
