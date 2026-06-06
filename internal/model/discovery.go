package model

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/samber/oops"
)

const (
	apiAnthropicMessages   = "anthropic-messages"
	apiOpenAICodexResponse = "openai-codex-responses"
	gpt52                  = "gpt-5.2"
)

// DiscoveryOptions configures remote model catalog discovery.
type DiscoveryOptions struct {
	Client       *http.Client
	CachePath    string
	SourceURL    string
	CacheTTL     time.Duration
	FetchTimeout time.Duration
	Enabled      bool
}

type discoveryProvider struct {
	Models map[string]discoveryModel `json:"models"`
	Name   string                    `json:"name"`
	API    string                    `json:"api"`
	ID     string                    `json:"id"`
}

type discoveryModel struct {
	Modalities *discoveryModalities `json:"modalities"`
	Limit      *discoveryLimit      `json:"limit"`
	Cost       *discoveryCost       `json:"cost"`
	Name       string               `json:"name"`
	ID         string               `json:"id"`
	ToolCall   bool                 `json:"tool_call"`
	Reasoning  bool                 `json:"reasoning"`
}

type discoveryModalities struct {
	Input []string `json:"input"`
}

type discoveryLimit struct {
	Context int `json:"context"`
	Output  int `json:"output"`
}

type discoveryCost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
}

type discoveryProviderMapping struct {
	Provider string
	Source   string
	API      string
	BaseURL  string
}

type discoveredCodexDefinition struct {
	ID     string
	Name   string
	Input  []InputMode
	Cost   Cost
	Window int
}

// DiscoverModels fetches model metadata from a models.dev-compatible API.
func DiscoverModels(ctx context.Context, options DiscoveryOptions) ([]Model, error) {
	if !options.Enabled {
		return []Model{}, nil
	}
	if strings.TrimSpace(options.SourceURL) == "" {
		return []Model{}, oops.In("model").Code("model_discovery_source").Errorf(
			"model discovery source URL is required",
		)
	}
	if options.CachePath != "" {
		return DiscoverModelsCached(ctx, CachedDiscoveryOptions{
			Client:       options.Client,
			CachePath:    options.CachePath,
			SourceURL:    options.SourceURL,
			CacheTTL:     options.CacheTTL,
			FetchTimeout: options.FetchTimeout,
			Enabled:      true,
		})
	}
	content, err := fetchDiscoveryCatalog(ctx, options)
	if err != nil {
		return []Model{}, err
	}

	return ParseDiscoveredModels(content)
}

// ParseDiscoveredModels parses a models.dev-compatible provider catalog into librecode models.
func ParseDiscoveredModels(content []byte) ([]Model, error) {
	var catalog map[string]discoveryProvider
	if err := json.Unmarshal(content, &catalog); err != nil {
		return []Model{}, oops.In("model").Code("model_discovery_decode").Wrapf(
			err,
			"decode model discovery catalog",
		)
	}

	models := []Model{}
	for _, mapping := range discoveryProviderMappings() {
		provider, ok := catalog[mapping.Source]
		if !ok {
			continue
		}
		models = append(models, discoveredProviderModels(provider.Models, mapping)...)
	}
	models = append(models, discoveredCodexModels()...)

	return models, nil
}

func fetchDiscoveryCatalog(ctx context.Context, options DiscoveryOptions) ([]byte, error) {
	client := options.Client
	if client == nil {
		client = http.DefaultClient
	}
	if options.FetchTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, options.FetchTimeout)
		defer cancel()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, options.SourceURL, http.NoBody)
	if err != nil {
		return nil, oops.In("model").Code("model_discovery_request").Wrapf(err, "create model discovery request")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "librecode model discovery")
	response, err := client.Do(request)
	if err != nil {
		return nil, oops.In("model").Code("model_discovery_fetch").Wrapf(err, "fetch model discovery catalog")
	}
	defer closeResponseBody(response.Body)
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, oops.In("model").Code("model_discovery_status").Errorf(
			"fetch model discovery catalog: unexpected status %s",
			response.Status,
		)
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return nil, oops.In("model").Code("model_discovery_read").Wrapf(err, "read model discovery catalog")
	}

	return content, nil
}

func discoveryProviderMappings() []discoveryProviderMapping {
	return []discoveryProviderMapping{
		mapping(providerAnthropic, "anthropic", apiAnthropicMessages, "https://api.anthropic.com"),
		mapping(providerAnthropicClaude, "anthropic", apiAnthropicMessages, "https://api.anthropic.com"),
		mapping(providerCerebras, "cerebras", apiOpenAICompletions, "https://api.cerebras.ai/v1"),
		mapping(providerDeepSeek, "deepseek", apiOpenAICompletions, "https://api.deepseek.com"),
		mapping(providerGroq, "groq", apiOpenAICompletions, "https://api.groq.com/openai/v1"),
		mapping(providerMistral, "mistral", apiOpenAICompletions, "https://api.mistral.ai/v1"),
		mapping(providerMoonshotAI, "moonshotai", apiOpenAICompletions, "https://api.moonshot.ai/v1"),
		mapping(providerMoonshotAICN, "moonshotai-cn", apiOpenAICompletions, "https://api.moonshot.cn/v1"),
		mapping(providerOpenAI, "openai", apiOpenAIResponses, "https://api.openai.com/v1"),
		mapping(providerOpenRouter, "openrouter", apiOpenAICompletions, "https://openrouter.ai/api/v1"),
		mapping(providerOpenCode, "opencode", apiOpenAICompletions, "https://opencode.ai/zen/v1"),
		mapping(providerOpenCodeGo, "opencode-go", apiOpenAICompletions, "https://opencode.ai/zen/go/v1"),
		mapping(providerVercelAIGateway, "vercel", apiOpenAICompletions, "https://ai-gateway.vercel.sh/v1"),
		mapping(providerXAI, "xai", apiOpenAICompletions, "https://api.x.ai/v1"),
		mapping(providerZAI, "zai-coding-plan", apiOpenAICompletions, "https://api.z.ai/api/coding/paas/v4"),
	}
}

func mapping(provider, source, api, baseURL string) discoveryProviderMapping {
	return discoveryProviderMapping{Provider: provider, Source: source, API: api, BaseURL: baseURL}
}

func discoveredProviderModels(definitions map[string]discoveryModel, mapping discoveryProviderMapping) []Model {
	models := make([]Model, 0, len(definitions))
	for modelID, definition := range definitions {
		if !definition.ToolCall {
			continue
		}
		models = append(models, modelFromDiscovery(modelID, &definition, mapping))
	}

	return models
}

func modelFromDiscovery(modelID string, definition *discoveryModel, mapping discoveryProviderMapping) Model {
	discoveryModelID := modelID
	if definition.ID != "" {
		discoveryModelID = definition.ID
	}
	name := definition.Name
	if name == "" {
		name = discoveryModelID
	}

	return Model{
		ThinkingLevelMap: thinkingLevelsForDiscoveredModel(mapping.Provider, discoveryModelID),
		Headers:          nil,
		Compat:           nil,
		Provider:         mapping.Provider,
		ID:               discoveryModelID,
		Name:             name,
		API:              mapping.API,
		BaseURL:          mapping.BaseURL,
		Input:            discoveryInputModes(definition.Modalities),
		Cost:             discoveryModelCost(definition.Cost),
		ContextWindow:    discoveryContextWindow(definition.Limit),
		MaxTokens:        discoveryMaxTokens(definition.Limit),
		Reasoning:        definition.Reasoning,
	}
}

func discoveryInputModes(modalities *discoveryModalities) []InputMode {
	if modalities == nil {
		return []InputMode{InputText}
	}
	input := []InputMode{InputText}
	if slices.Contains(modalities.Input, string(InputImage)) {
		input = append(input, InputImage)
	}

	return input
}

func discoveryModelCost(cost *discoveryCost) Cost {
	if cost == nil {
		return Cost{Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0}
	}

	return Cost{Input: cost.Input, Output: cost.Output, CacheRead: cost.CacheRead, CacheWrite: cost.CacheWrite}
}

func discoveryContextWindow(limit *discoveryLimit) int {
	if limit == nil {
		return 0
	}

	return limit.Context
}

func discoveryMaxTokens(limit *discoveryLimit) int {
	if limit == nil {
		return 0
	}

	return limit.Output
}

func discoveredCodexModels() []Model {
	metadata := openAICodexMetadata()
	definitions := discoveredCodexDefinitions(metadata.ContextWindow)
	models := make([]Model, 0, len(definitions))
	for _, definition := range definitions {
		models = append(models, Model{
			ThinkingLevelMap: thinkingLevelsForDiscoveredModel(providerOpenAICodex, definition.ID),
			Headers:          nil,
			Compat:           cloneAnyMap(metadata.Compat),
			Provider:         providerOpenAICodex,
			ID:               definition.ID,
			Name:             definition.Name,
			API:              metadata.API,
			BaseURL:          metadata.BaseURL,
			Input:            append([]InputMode{}, definition.Input...),
			Cost:             definition.Cost,
			ContextWindow:    definition.Window,
			MaxTokens:        metadata.MaxTokens,
			Reasoning:        metadata.Reasoning,
		})
	}

	return models
}

func discoveredCodexDefinitions(defaultWindow int) []discoveredCodexDefinition {
	textImage := []InputMode{InputText, InputImage}

	return []discoveredCodexDefinition{
		codexDefinition(gpt52, "GPT-5.2", textImage, defaultWindow, 1.75, 14, 0.175),
		codexDefinition("gpt-5.3-codex", "GPT-5.3 Codex", textImage, defaultWindow, 1.75, 14, 0.175),
		codexDefinition("gpt-5.3-codex-spark", "GPT-5.3 Codex Spark", []InputMode{InputText}, 128000, 1.75, 14, 0.175),
		codexDefinition(gpt54, "GPT-5.4", textImage, defaultWindow, 2.5, 15, 0.25),
		codexDefinition("gpt-5.4-mini", "GPT-5.4 mini", textImage, defaultWindow, 0.75, 4.5, 0.075),
		codexDefinition(gpt55, "GPT-5.5", textImage, defaultWindow, 5, 30, 0.5),
	}
}

func codexDefinition(
	modelID string,
	name string,
	input []InputMode,
	window int,
	inputCost float64,
	outputCost float64,
	cacheReadCost float64,
) discoveredCodexDefinition {
	return discoveredCodexDefinition{
		Cost:   Cost{Input: inputCost, Output: outputCost, CacheRead: cacheReadCost, CacheWrite: 0},
		Input:  append([]InputMode{}, input...),
		ID:     modelID,
		Name:   name,
		Window: window,
	}
}

func thinkingLevelsForDiscoveredModel(provider, modelID string) map[ThinkingLevel]*string {
	levels := map[ThinkingLevel]*string{}
	addOpenAIThinkingOff(levels, provider, modelID)
	addOpenAIThinkingNone(levels, provider, modelID)
	addOpenAIXHigh(levels, modelID)
	if len(levels) == 0 {
		return nil
	}

	return levels
}

func addOpenAIThinkingOff(levels map[ThinkingLevel]*string, provider, modelID string) {
	if (provider == providerOpenAI || provider == providerOpenAICodex) && strings.HasPrefix(modelID, "gpt-5") {
		levels[ThinkingOff] = nil
	}
}

func addOpenAIThinkingNone(levels map[ThinkingLevel]*string, provider, modelID string) {
	if provider != providerOpenAI || !openAIResponsesNoReasoningModel(modelID) {
		return
	}
	none := "none"
	levels[ThinkingOff] = &none
}

func addOpenAIXHigh(levels map[ThinkingLevel]*string, modelID string) {
	if !openAISupportsXHigh(modelID) {
		return
	}
	xhigh := "xhigh"
	levels[ThinkingXHigh] = &xhigh
}

func openAISupportsXHigh(modelID string) bool {
	return strings.Contains(modelID, gpt52) ||
		strings.Contains(modelID, "gpt-5.3") ||
		strings.Contains(modelID, "gpt-5.4") ||
		strings.Contains(modelID, gpt55)
}

func closeResponseBody(body io.Closer) {
	if err := body.Close(); err != nil {
		_ = err
	}
}

func openAIResponsesNoReasoningModel(modelID string) bool {
	switch modelID {
	case "gpt-5.1", gpt52, "gpt-5.3-codex", gpt54, "gpt-5.4-mini", "gpt-5.4-nano", gpt55:
		return true
	default:
		return false
	}
}
