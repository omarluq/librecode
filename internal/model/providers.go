package model

import (
	"slices"

	"github.com/omarluq/librecode/internal/anthropicmodel"
)

const (
	providerAnthropic            = "anthropic"
	providerAnthropicClaude      = "anthropic-claude"
	providerAzureOpenAIResponses = "azure-openai-responses"
	providerCerebras             = "cerebras"
	providerDeepSeek             = "deepseek"
	providerGroq                 = "groq"
	providerMistral              = "mistral"
	providerMoonshotAI           = "moonshotai"
	providerMoonshotAICN         = "moonshotai-cn"
	providerOpenAI               = "openai"
	providerOpenAICodex          = "openai-codex"
	providerOpenCode             = "opencode"
	providerOpenCodeGo           = "opencode-go"
	providerOpenRouter           = "openrouter"
	providerVercelAIGateway      = "vercel-ai-gateway"
	providerXAI                  = "xai"
	providerZAI                  = "zai"
	apiOpenAICompletions         = "openai-completions"
	apiOpenAIResponses           = "openai-responses"
	gpt54                        = "gpt-5.4"
	gpt55                        = "gpt-5.5"
	kimiK26                      = "kimi-k2.6"
)

type providerDisplayPair struct {
	Provider string
	Display  string
}

type providerModelPair struct {
	Provider string
	ModelID  string
}

// ProviderDisplayNames maps built-in provider IDs to user-facing names.
func ProviderDisplayNames() map[string]string {
	return providerDisplayNameMap()
}

// DefaultModelPerProvider maps supported provider IDs to librecode's default model IDs.
func DefaultModelPerProvider() map[string]string {
	return defaultModelMap()
}

// BuiltInModels returns a deterministic built-in model catalog.
func BuiltInModels() []Model {
	defaultModels := DefaultModelPerProvider()

	providers := make([]string, 0, len(defaultModels))
	for provider := range defaultModels {
		providers = append(providers, provider)
	}

	slices.Sort(providers)

	models := make([]Model, 0, len(providers)+len(additionalBuiltInModels()))
	for _, provider := range providers {
		models = append(models, builtInDefaultModel(provider, defaultModels[provider]))
	}

	for _, pair := range additionalBuiltInModels() {
		models = append(models, builtInDefaultModel(pair.Provider, pair.ModelID))
	}

	return models
}

func additionalBuiltInModels() []providerModelPair {
	return []providerModelPair{
		{Provider: providerAnthropic, ModelID: anthropicmodel.Mythos5},
		{Provider: providerAnthropicClaude, ModelID: anthropicmodel.Mythos5},
	}
}

func builtInDefaultModel(provider, modelID string) Model {
	metadata := defaultProviderMetadata()[provider]

	return Model{
		ThinkingLevelMap: metadata.ThinkingLevelMap,
		Headers:          metadata.Headers,
		Compat:           metadata.Compat,
		Provider:         provider,
		ID:               modelID,
		Name:             modelID,
		API:              metadata.API,
		BaseURL:          metadata.BaseURL,
		Input:            []InputMode{InputText},
		Cost:             Cost{Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0},
		ContextWindow:    metadata.ContextWindow,
		MaxTokens:        metadata.MaxTokens,
		Reasoning:        metadata.Reasoning,
	}
}

type providerMetadata struct {
	ThinkingLevelMap map[ThinkingLevel]*string
	Headers          map[string]string
	Compat           map[string]any
	API              string
	BaseURL          string
	ContextWindow    int
	MaxTokens        int
	Reasoning        bool
}

func defaultProviderMetadata() map[string]providerMetadata {
	return map[string]providerMetadata{
		providerAnthropic:            anthropicMetadata(),
		providerAnthropicClaude:      anthropicMetadata(),
		providerAzureOpenAIResponses: azureOpenAIMetadata(),
		providerCerebras:             openAICompatibleMetadata("https://api.cerebras.ai/v1", true),
		providerDeepSeek:             openAICompatibleMetadata("https://api.deepseek.com", true),
		providerGroq:                 openAICompatibleMetadata("https://api.groq.com/openai/v1", false),
		providerMistral:              openAICompatibleMetadata("https://api.mistral.ai/v1", false),
		providerMoonshotAI:           openAICompatibleMetadata("https://api.moonshot.ai/v1", false),
		providerMoonshotAICN:         openAICompatibleMetadata("https://api.moonshot.cn/v1", false),
		providerOpenAI:               openAIResponsesMetadata(),
		providerOpenAICodex:          openAICodexMetadata(),
		providerOpenCode:             openAICompatibleMetadata("https://opencode.ai/zen/v1", true),
		providerOpenCodeGo:           openAICompatibleMetadata("https://opencode.ai/zen/go/v1", true),
		providerOpenRouter:           openAICompatibleMetadata("https://openrouter.ai/api/v1", false),
		providerVercelAIGateway:      openAICompatibleMetadata("https://ai-gateway.vercel.sh/v1", true),
		providerXAI:                  openAICompatibleMetadata("https://api.x.ai/v1", true),
		providerZAI:                  openAICompatibleMetadata("https://api.z.ai/api/coding/paas/v4", true),
	}
}

func openAICompatibleMetadata(baseURL string, reasoning bool) providerMetadata {
	return providerMetadata{
		ThinkingLevelMap: nil,
		Headers:          nil,
		Compat:           nil,
		API:              apiOpenAICompletions,
		BaseURL:          baseURL,
		ContextWindow:    0,
		MaxTokens:        0,
		Reasoning:        reasoning,
	}
}

const (
	openAICodexContextWindow = 272_000
	anthropicContextWindow   = 1_000_000
	largeMaxOutputTokens     = 128_000
)

func openAIResponsesMetadata() providerMetadata {
	return providerMetadata{
		ThinkingLevelMap: nil,
		Headers:          nil,
		Compat:           nil,
		API:              apiOpenAIResponses,
		BaseURL:          "https://api.openai.com/v1",
		ContextWindow:    0,
		MaxTokens:        0,
		Reasoning:        true,
	}
}

func openAICodexMetadata() providerMetadata {
	return providerMetadata{
		ThinkingLevelMap: nil,
		Headers:          nil,
		Compat:           nil,
		API:              "openai-codex-responses",
		BaseURL:          "https://chatgpt.com/backend-api",
		ContextWindow:    openAICodexContextWindow,
		MaxTokens:        largeMaxOutputTokens,
		Reasoning:        true,
	}
}

func anthropicMetadata() providerMetadata {
	xhigh := string(ThinkingXHigh)

	return providerMetadata{
		ThinkingLevelMap: map[ThinkingLevel]*string{ThinkingOff: nil, ThinkingXHigh: &xhigh},
		Headers:          nil,
		Compat:           nil,
		API:              "anthropic-messages",
		BaseURL:          "https://api.anthropic.com",
		ContextWindow:    anthropicContextWindow,
		MaxTokens:        largeMaxOutputTokens,
		Reasoning:        true,
	}
}

func azureOpenAIMetadata() providerMetadata {
	return providerMetadata{
		ThinkingLevelMap: nil,
		Headers:          nil,
		Compat:           nil,
		API:              apiOpenAIResponses,
		BaseURL:          "",
		ContextWindow:    0,
		MaxTokens:        0,
		Reasoning:        true,
	}
}

func providerDisplayNameMap() map[string]string {
	pairs := []providerDisplayPair{
		{Provider: providerAnthropic, Display: "Anthropic API"},
		{Provider: providerAnthropicClaude, Display: "Claude Pro/Max (Anthropic OAuth)"},
		{Provider: providerAzureOpenAIResponses, Display: "Azure OpenAI Responses"},
		{Provider: providerCerebras, Display: "Cerebras"},
		{Provider: providerDeepSeek, Display: "DeepSeek"},
		{Provider: providerGroq, Display: "Groq"},
		{Provider: providerMistral, Display: "Mistral"},
		{Provider: providerMoonshotAI, Display: "Moonshot AI"},
		{Provider: providerMoonshotAICN, Display: "Moonshot AI (China)"},
		{Provider: providerOpenAI, Display: "OpenAI"},
		{Provider: providerOpenAICodex, Display: "ChatGPT Plus/Pro (Codex)"},
		{Provider: providerOpenCode, Display: "OpenCode Zen"},
		{Provider: providerOpenCodeGo, Display: "OpenCode Go"},
		{Provider: providerOpenRouter, Display: "OpenRouter"},
		{Provider: providerVercelAIGateway, Display: "Vercel AI Gateway"},
		{Provider: providerXAI, Display: "xAI"},
		{Provider: providerZAI, Display: "ZAI"},
	}

	return displayPairsToMap(pairs)
}

func defaultModelMap() map[string]string {
	pairs := []providerModelPair{
		{Provider: providerAnthropic, ModelID: anthropicmodel.Fable5},
		{Provider: providerAnthropicClaude, ModelID: anthropicmodel.Fable5},
		{Provider: providerAzureOpenAIResponses, ModelID: gpt54},
		{Provider: providerCerebras, ModelID: "zai-glm-4.7"},
		{Provider: providerDeepSeek, ModelID: "deepseek-v4-pro"},
		{Provider: providerGroq, ModelID: "openai/gpt-oss-120b"},
		{Provider: providerMistral, ModelID: "devstral-medium-latest"},
		{Provider: providerMoonshotAI, ModelID: kimiK26},
		{Provider: providerMoonshotAICN, ModelID: kimiK26},
		{Provider: providerOpenAI, ModelID: gpt54},
		{Provider: providerOpenAICodex, ModelID: gpt55},
		{Provider: providerOpenCode, ModelID: gpt55},
		{Provider: providerOpenCodeGo, ModelID: kimiK26},
		{Provider: providerOpenRouter, ModelID: "moonshotai/kimi-k2.6"},
		{Provider: providerVercelAIGateway, ModelID: "zai/glm-5.1"},
		{Provider: providerXAI, ModelID: "grok-4.20-0309-reasoning"},
		{Provider: providerZAI, ModelID: "glm-5.1"},
	}

	return modelPairsToMap(pairs)
}

func displayPairsToMap(pairs []providerDisplayPair) map[string]string {
	result := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		result[pair.Provider] = pair.Display
	}

	return result
}

func modelPairsToMap(pairs []providerModelPair) map[string]string {
	result := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		result[pair.Provider] = pair.ModelID
	}

	return result
}
