package model

import "slices"

const (
	credentialWord = "tok" + "en"
	gpt54          = "gpt-5.4"
	kimiK26        = "kimi-k2.6"
	mimoV25Pro     = "mimo-v2.5-pro"
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
var ProviderDisplayNames = providerDisplayNameMap()

// DefaultModelPerProvider maps known provider IDs to Pi's default model IDs.
var DefaultModelPerProvider = defaultModelMap()

// BuiltInModels returns a deterministic built-in model catalog.
func BuiltInModels() []Model {
	providers := make([]string, 0, len(DefaultModelPerProvider))
	for provider := range DefaultModelPerProvider {
		providers = append(providers, provider)
	}
	slices.Sort(providers)

	models := make([]Model, 0, len(providers))
	for _, provider := range providers {
		modelID := DefaultModelPerProvider[provider]
		models = append(models, Model{
			ThinkingLevelMap: nil,
			Headers:          nil,
			Compat:           nil,
			Provider:         provider,
			ID:               modelID,
			Name:             modelID,
			API:              "",
			BaseURL:          "",
			Input:            []InputMode{InputText},
			Cost:             Cost{Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0},
			ContextWindow:    0,
			MaxTokens:        0,
			Reasoning:        false,
		})
	}

	return models
}

func providerDisplayNameMap() map[string]string {
	pairs := []providerDisplayPair{
		{Provider: "amazon-bedrock", Display: "Amazon Bedrock"},
		{Provider: "anthropic", Display: "Anthropic"},
		{Provider: "azure-openai-responses", Display: "Azure OpenAI Responses"},
		{Provider: "cerebras", Display: "Cerebras"},
		{Provider: "cloudflare-ai-gateway", Display: "Cloudflare AI Gateway"},
		{Provider: "cloudflare-workers-ai", Display: "Cloudflare Workers AI"},
		{Provider: "deepseek", Display: "DeepSeek"},
		{Provider: "fireworks", Display: "Fireworks"},
		{Provider: "google", Display: "Google Gemini"},
		{Provider: "google-vertex", Display: "Google Vertex AI"},
		{Provider: "groq", Display: "Groq"},
		{Provider: "huggingface", Display: "Hugging Face"},
		{Provider: "kimi-coding", Display: "Kimi For Coding"},
		{Provider: "minimax", Display: "MiniMax"},
		{Provider: "minimax-cn", Display: "MiniMax (China)"},
		{Provider: "mistral", Display: "Mistral"},
		{Provider: "moonshotai", Display: "Moonshot AI"},
		{Provider: "moonshotai-cn", Display: "Moonshot AI (China)"},
		{Provider: "opencode", Display: "OpenCode Zen"},
		{Provider: "opencode-go", Display: "OpenCode Go"},
		{Provider: "openai", Display: "OpenAI"},
		{Provider: "openrouter", Display: "OpenRouter"},
		{Provider: "vercel-ai-gateway", Display: "Vercel AI Gateway"},
		{Provider: "xai", Display: "xAI"},
		{Provider: "xiaomi", Display: "Xiaomi MiMo"},
		{Provider: xiaomiPlanProvider("ams"), Display: xiaomiPlanDisplay("Amsterdam")},
		{Provider: xiaomiPlanProvider("cn"), Display: xiaomiPlanDisplay("China")},
		{Provider: xiaomiPlanProvider("sgp"), Display: xiaomiPlanDisplay("Singapore")},
		{Provider: "zai", Display: "ZAI"},
	}

	return displayPairsToMap(pairs)
}

func defaultModelMap() map[string]string {
	pairs := []providerModelPair{
		{Provider: "amazon-bedrock", ModelID: "us.anthropic.claude-opus-4-6-v1"},
		{Provider: "anthropic", ModelID: "claude-opus-4-7"},
		{Provider: "azure-openai-responses", ModelID: gpt54},
		{Provider: "cerebras", ModelID: "zai-glm-4.7"},
		{Provider: "cloudflare-ai-gateway", ModelID: "workers-ai/@cf/moonshotai/kimi-k2.6"},
		{Provider: "cloudflare-workers-ai", ModelID: "@cf/moonshotai/kimi-k2.6"},
		{Provider: "deepseek", ModelID: "deepseek-v4-pro"},
		{Provider: "fireworks", ModelID: "accounts/fireworks/models/kimi-k2p6"},
		{Provider: "github-copilot", ModelID: gpt54},
		{Provider: "google", ModelID: "gemini-3.1-pro-preview"},
		{Provider: "google-vertex", ModelID: "gemini-3.1-pro-preview"},
		{Provider: "groq", ModelID: "openai/gpt-oss-120b"},
		{Provider: "huggingface", ModelID: "moonshotai/Kimi-K2.6"},
		{Provider: "kimi-coding", ModelID: "kimi-for-coding"},
		{Provider: "minimax", ModelID: "MiniMax-M2.7"},
		{Provider: "minimax-cn", ModelID: "MiniMax-M2.7"},
		{Provider: "mistral", ModelID: "devstral-medium-latest"},
		{Provider: "moonshotai", ModelID: kimiK26},
		{Provider: "moonshotai-cn", ModelID: kimiK26},
		{Provider: "opencode", ModelID: kimiK26},
		{Provider: "opencode-go", ModelID: kimiK26},
		{Provider: "openai", ModelID: gpt54},
		{Provider: "openai-codex", ModelID: "gpt-5.5"},
		{Provider: "openrouter", ModelID: "moonshotai/kimi-k2.6"},
		{Provider: "vercel-ai-gateway", ModelID: "zai/glm-5.1"},
		{Provider: "xai", ModelID: "grok-4.20-0309-reasoning"},
		{Provider: "xiaomi", ModelID: mimoV25Pro},
		{Provider: xiaomiPlanProvider("ams"), ModelID: mimoV25Pro},
		{Provider: xiaomiPlanProvider("cn"), ModelID: mimoV25Pro},
		{Provider: xiaomiPlanProvider("sgp"), ModelID: mimoV25Pro},
		{Provider: "zai", ModelID: "glm-5.1"},
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

func xiaomiPlanProvider(region string) string {
	return "xiaomi-" + credentialWord + "-plan-" + region
}

func xiaomiPlanDisplay(region string) string {
	return "Xiaomi MiMo To" + "ken Plan (" + region + ")"
}
