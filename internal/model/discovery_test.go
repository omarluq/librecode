package model_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/anthropicmodel"
	"github.com/omarluq/librecode/internal/model"
)

const (
	testDiscoveryOpenAI             = "openai"
	testDiscoveryGPT54              = "gpt-5.4"
	testDiscoveryGPT55              = "gpt-5.5"
	testDiscoveryOpenAIResponsesAPI = "openai-responses"
)

func TestParseDiscoveredModelsMapsSupportedProviders(t *testing.T) {
	t.Parallel()

	content := []byte(`{
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
				"text-only": {"tool_call": false}
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
		}
	}`)

	models, err := model.ParseDiscoveredModels(content)
	require.NoError(t, err)
	openAIModel := findModel(t, models, testDiscoveryOpenAI, testDiscoveryGPT54)
	assert.Equal(t, testDiscoveryOpenAIResponsesAPI, openAIModel.API)
	assert.Equal(t, "https://api.openai.com/v1", openAIModel.BaseURL)
	assert.Equal(t, 272000, openAIModel.ContextWindow)
	assert.Equal(t, 128000, openAIModel.MaxTokens)
	assert.Equal(t, []model.InputMode{model.InputText, model.InputImage}, openAIModel.Input)
	assert.True(t, openAIModel.Reasoning)
	assert.Equal(t, 2.5, openAIModel.Cost.Input)
	assert.Equal(t, 15.0, openAIModel.Cost.Output)
	assert.NotNil(t, openAIModel.ThinkingLevelMap[model.ThinkingOff])

	openCodeModel := findModel(t, models, "opencode", testDiscoveryGPT55)
	assert.Equal(t, "openai-completions", openCodeModel.API)
	assert.Equal(t, "https://opencode.ai/zen/v1", openCodeModel.BaseURL)
	assert.Equal(t, []model.InputMode{model.InputText}, openCodeModel.Input)
	assert.NotContains(t, modelIDsForProvider(models, testDiscoveryOpenAI), "text-only")

	anthropicOAuthModel := findModel(t, models, "anthropic-claude", anthropicmodel.Fable5)
	assert.Equal(t, "anthropic-messages", anthropicOAuthModel.API)
	assert.Equal(t, "https://api.anthropic.com", anthropicOAuthModel.BaseURL)
	assert.Contains(t, anthropicOAuthModel.ThinkingLevelMap, model.ThinkingOff)
	assert.Contains(t, anthropicOAuthModel.ThinkingLevelMap, model.ThinkingXHigh)

	codexModel := findModel(t, models, "openai-codex", testDiscoveryGPT55)
	assert.Equal(t, "openai-codex-responses", codexModel.API)
	assert.Equal(t, "https://chatgpt.com/backend-api", codexModel.BaseURL)
	assert.Equal(t, 272000, codexModel.ContextWindow)
}

func TestRegistryDiscoveryMergesBeforeCustomOverrides(t *testing.T) {
	t.Parallel()

	discovered := []model.Model{{
		ThinkingLevelMap: nil,
		Headers:          nil,
		Compat:           nil,
		Provider:         testDiscoveryOpenAI,
		ID:               testDiscoveryGPT54,
		Name:             "Discovered GPT",
		API:              testDiscoveryOpenAIResponsesAPI,
		BaseURL:          "https://api.openai.com/v1",
		Input:            []model.InputMode{model.InputText},
		Cost:             model.Cost{Input: 1, Output: 2, CacheRead: 0, CacheWrite: 0},
		ContextWindow:    128000,
		MaxTokens:        32000,
		Reasoning:        true,
	}}
	custom := []model.Model{{
		ThinkingLevelMap: nil,
		Headers:          nil,
		Compat:           nil,
		Provider:         testDiscoveryOpenAI,
		ID:               testDiscoveryGPT54,
		Name:             "Custom GPT",
		API:              testDiscoveryOpenAIResponsesAPI,
		BaseURL:          "https://custom.invalid/v1",
		Input:            []model.InputMode{model.InputText, model.InputImage},
		Cost:             model.Cost{Input: 3, Output: 4, CacheRead: 0, CacheWrite: 0},
		ContextWindow:    256000,
		MaxTokens:        64000,
		Reasoning:        true,
	}}

	merged := model.MergeModelCatalogsForTest(
		[]model.Model{testModel(testDiscoveryOpenAI, testDiscoveryGPT54, "Built-in GPT")},
		discovered,
		custom,
	)
	require.Len(t, merged, 1)
	assert.Equal(t, "Custom GPT", merged[0].Name)
	assert.Equal(t, "https://custom.invalid/v1", merged[0].BaseURL)
	assert.Equal(t, 256000, merged[0].ContextWindow)
}

func findModel(t *testing.T, models []model.Model, provider, modelID string) model.Model {
	t.Helper()

	for index := range models {
		candidate := models[index]
		if candidate.Provider == provider && candidate.ID == modelID {
			return candidate
		}
	}
	require.Failf(t, "model not found", "%s/%s", provider, modelID)

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

func modelIDsForProvider(models []model.Model, provider string) []string {
	ids := []string{}
	for index := range models {
		if models[index].Provider == provider {
			ids = append(ids, models[index].ID)
		}
	}

	return ids
}
