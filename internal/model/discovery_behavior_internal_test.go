package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/anthropicmodel"
)

const (
	testDiscoveryOpenAI             = "openai"
	testDiscoveryGPT54              = "gpt-5.4"
	testDiscoveryGPT55              = "gpt-5.5"
	testDiscoveryOpenAIResponsesAPI = "openai-responses"
)

func TestParseDiscoveredModelsMapsSupportedProviders(t *testing.T) {
	t.Parallel()

	models, err := ParseDiscoveredModels(supportedProvidersDiscoveryFixture())
	require.NoError(t, err)

	assertDiscoveredOpenAIModels(t, models)
	assertDiscoveredOpenCodeModel(t, models)
	assertDiscoveredAnthropicOAuthModel(t, models)
	assertDiscoveredCodexModels(t, models)
	assertDiscoveredZAIModel(t, models)
}

func assertDiscoveredOpenAIModels(t *testing.T, models []Model) {
	t.Helper()

	openAIModel := findModel(t, models, testDiscoveryOpenAI, testDiscoveryGPT54)
	assert.Equal(t, testDiscoveryOpenAIResponsesAPI, openAIModel.API)
	assert.Equal(t, "https://api.openai.com/v1", openAIModel.BaseURL)
	assert.Equal(t, 272000, openAIModel.ContextWindow)
	assert.Equal(t, 128000, openAIModel.MaxTokens)
	assert.Equal(t, []InputMode{InputText, InputImage}, openAIModel.Input)
	assert.True(t, openAIModel.Reasoning)
	assert.InDelta(t, 2.5, openAIModel.Cost.Input, 0)
	assert.InDelta(t, 15.0, openAIModel.Cost.Output, 0)
	assert.NotNil(t, openAIModel.ThinkingLevelMap[ThinkingOff])
	assert.NotContains(t, modelIDsForProvider(models, testDiscoveryOpenAI), "gpt-5.6")
	assert.NotContains(t, modelIDsForProvider(models, testDiscoveryOpenAI), "text-only")

	openAIGPT56Model := findModel(t, models, testDiscoveryOpenAI, gpt56Sol)
	assert.Equal(t, "GPT-5.6 Sol", openAIGPT56Model.Name)
	assert.Equal(t, openAIResponsesContextWindow, openAIGPT56Model.ContextWindow)
	assert.Equal(t, largeMaxOutputTokens, openAIGPT56Model.MaxTokens)
	assert.Equal(t, Cost{Input: 5, Output: 30, CacheRead: 0.5, CacheWrite: 6.25}, openAIGPT56Model.Cost)
	assertDiscoveredThinkingLevelMapping(t, &openAIGPT56Model, ThinkingMax, "max")
}

func assertDiscoveredOpenCodeModel(t *testing.T, models []Model) {
	t.Helper()

	openCodeModel := findModel(t, models, "opencode", testDiscoveryGPT55)
	assert.Equal(t, "openai-completions", openCodeModel.API)
	assert.Equal(t, "https://opencode.ai/zen/v1", openCodeModel.BaseURL)
	assert.Equal(t, []InputMode{InputText}, openCodeModel.Input)
}

func assertDiscoveredAnthropicOAuthModel(t *testing.T, models []Model) {
	t.Helper()

	anthropicOAuthModel := findModel(t, models, "anthropic-claude", anthropicmodel.Fable5)
	assert.Equal(t, "anthropic-messages", anthropicOAuthModel.API)
	assert.Equal(t, "https://api.anthropic.com", anthropicOAuthModel.BaseURL)
	assert.Contains(t, anthropicOAuthModel.ThinkingLevelMap, ThinkingOff)
	xhighLevel := anthropicOAuthModel.ThinkingLevelMap[ThinkingXHigh]
	require.NotNil(t, xhighLevel)
	assert.Equal(t, string(ThinkingXHigh), *xhighLevel)
}

func assertDiscoveredCodexModels(t *testing.T, models []Model) {
	t.Helper()

	codexModel := findModel(t, models, "openai-codex", testDiscoveryGPT55)
	assert.Equal(t, "openai-codex-responses", codexModel.API)
	assert.Equal(t, "https://chatgpt.com/backend-api", codexModel.BaseURL)
	assert.Equal(t, 272000, codexModel.ContextWindow)

	codexGPT56Model := findModel(t, models, "openai-codex", gpt56Sol)
	assert.Equal(t, "GPT-5.6 Sol", codexGPT56Model.Name)
	assert.Equal(t, 372000, codexGPT56Model.ContextWindow)
	assert.Equal(t, 128000, codexGPT56Model.MaxTokens)
	assert.Equal(t, Cost{Input: 5, Output: 30, CacheRead: 0.5, CacheWrite: 6.25}, codexGPT56Model.Cost)
	assertDiscoveredThinkingLevelMapping(t, &codexGPT56Model, ThinkingMax, "max")
	assertDiscoveredThinkingLevelMapping(t, &codexGPT56Model, ThinkingMinimal, "low")
}

func assertDiscoveredZAIModel(t *testing.T, models []Model) {
	t.Helper()

	zaiModel := findModel(t, models, "zai", "glm-5.2")
	assert.Equal(t, "openai-completions", zaiModel.API)
	assert.Equal(t, "https://api.z.ai/api/coding/paas/v4", zaiModel.BaseURL)
	assert.Equal(t, 1_000_000, zaiModel.ContextWindow)
	assert.Equal(t, 131_072, zaiModel.MaxTokens)
	assert.True(t, zaiModel.Reasoning)
	assertDiscoveredThinkingLevelMapping(t, &zaiModel, ThinkingXHigh, "max")
}

func assertDiscoveredThinkingLevelMapping(t *testing.T, discovered *Model, level ThinkingLevel, expected string) {
	t.Helper()

	mapped := discovered.ThinkingLevelMap[level]
	require.NotNil(t, mapped)
	assert.Equal(t, expected, *mapped)
}

func supportedProvidersDiscoveryFixture() []byte {
	return []byte(`{
		"openai": {
			"models": {
				"gpt-5.4": {
					"id": "gpt-5.4",
					"name": "GPT-5.4",
					"tool_call": true,
					"reasoning": true,
					"modalities": {"input": ["text", "image"]},
					"limit": {"context": 272000, "output": 128000},
					"cost": {"input": 2.5, "output": 15, "cache_read": 0.25, "cache_write": 0}
				},
				"text-only": {"tool_call": false},
				"gpt-5.6": {"id":"gpt-5.6", "name":"GPT-5.6", "tool_call": true, "reasoning": true}
			}
		},
		"anthropic": {
			"models": {
				"` + anthropicmodel.Fable5 + `": {
					"name": "Claude Fable 5",
					"tool_call": true,
					"reasoning": true,
					"modalities": {"input": ["text", "image"]},
					"limit": {"context": 1000000, "output": 128000}
				}
			}
		},
		"opencode": {
			"models": {
				"gpt-5.5": {
					"name": "GPT-5.5",
					"tool_call": true,
					"reasoning": true,
					"modalities": {"input": ["text"]},
					"limit": {"context": 272000, "output": 128000}
				}
			}
		},
		"zai-coding-plan": {
			"models": {
				"glm-5.2": {
					"name": "GLM-5.2",
					"tool_call": true,
					"reasoning": true,
					"modalities": {"input": ["text"]},
					"limit": {"context": 1000000, "output": 131072}
				}
			}
		}
	}`)
}

func TestRegistryDiscoveryMergesBeforeCustomOverrides(t *testing.T) {
	t.Parallel()

	discovered := []Model{{
		ThinkingLevelMap: nil,
		Headers:          nil,
		Compat:           nil,
		Provider:         testDiscoveryOpenAI,
		ID:               testDiscoveryGPT54,
		Name:             "Discovered GPT",
		API:              testDiscoveryOpenAIResponsesAPI,
		BaseURL:          "https://api.openai.com/v1",
		Input:            []InputMode{InputText},
		Cost:             Cost{Input: 1, Output: 2, CacheRead: 0, CacheWrite: 0},
		ContextWindow:    128000,
		MaxTokens:        32000,
		Reasoning:        true,
	}}
	custom := []Model{{
		ThinkingLevelMap: nil,
		Headers:          nil,
		Compat:           nil,
		Provider:         testDiscoveryOpenAI,
		ID:               testDiscoveryGPT54,
		Name:             "Custom GPT",
		API:              testDiscoveryOpenAIResponsesAPI,
		BaseURL:          "https://custom.invalid/v1",
		Input:            []InputMode{InputText, InputImage},
		Cost:             Cost{Input: 3, Output: 4, CacheRead: 0, CacheWrite: 0},
		ContextWindow:    256000,
		MaxTokens:        64000,
		Reasoning:        true,
	}}

	merged := mergeModelCatalogs(
		[]Model{discoveryTestModel(testDiscoveryOpenAI, testDiscoveryGPT54, "Built-in GPT")},
		discovered,
		custom,
	)
	require.Len(t, merged, 1)
	assert.Equal(t, "Custom GPT", merged[0].Name)
	assert.Equal(t, "https://custom.invalid/v1", merged[0].BaseURL)
	assert.Equal(t, 256000, merged[0].ContextWindow)
}

func findModel(t *testing.T, models []Model, provider, modelID string) Model {
	t.Helper()

	for index := range models {
		candidate := models[index]
		if candidate.Provider == provider && candidate.ID == modelID {
			return candidate
		}
	}

	require.Failf(t, "model not found", "%s/%s", provider, modelID)

	return Model{
		ThinkingLevelMap: nil,
		Headers:          nil,
		Compat:           nil,
		Provider:         "",
		ID:               "",
		Name:             "",
		API:              "",
		BaseURL:          "",
		Input:            nil,
		Cost:             Cost{Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0},
		ContextWindow:    0,
		MaxTokens:        0,
		Reasoning:        false,
	}
}

func modelIDsForProvider(models []Model, provider string) []string {
	ids := []string{}

	for index := range models {
		if models[index].Provider == provider {
			ids = append(ids, models[index].ID)
		}
	}

	return ids
}

func discoveryTestModel(provider, modelID, name string) Model {
	return Model{
		ThinkingLevelMap: nil,
		Headers:          nil,
		Compat:           nil,
		Provider:         provider,
		ID:               modelID,
		Name:             name,
		API:              "",
		BaseURL:          "",
		Input:            []InputMode{InputText},
		Cost:             Cost{Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0},
		ContextWindow:    0,
		MaxTokens:        0,
		Reasoning:        false,
	}
}
