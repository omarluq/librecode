package model

import (
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	xetag "github.com/charmbracelet/x/etag"
	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/anthropicmodel"
	"github.com/omarluq/librecode/internal/units"
)

const (
	apiAnthropicMessages = "anthropic-messages"
	gpt52                = "gpt-5.2"
	gpt52InputCost       = 1.75
	gpt52OutputCost      = 14
	gpt52CacheCost       = 0.175
	gpt54InputCost       = 2.5
	gpt54OutputCost      = 15
	gpt54CacheCost       = 0.25
	gpt54MiniInput       = 0.75
	gpt54MiniOutput      = 4.5
	gpt54MiniCache       = 0.075
	gpt55InputCost       = 5
	gpt55OutputCost      = 30
	gpt55CacheCost       = 0.5
	gpt56Sol             = "gpt-5.6-sol"
	gpt56Terra           = "gpt-5.6-terra"
	gpt56Luna            = "gpt-5.6-luna"
	gpt56SolInputCost    = 5
	gpt56SolOutputCost   = 30
	gpt56SolCacheCost    = 0.5
	gpt56SolCacheWrite   = 6.25
	gpt56TerraInputCost  = 2.5
	gpt56TerraOutputCost = 15
	gpt56TerraCacheCost  = 0.25
	gpt56TerraCacheWrite = 3.125
	gpt56LunaInputCost   = 1
	gpt56LunaOutputCost  = 6
	gpt56LunaCacheCost   = 0.1
	gpt56LunaCacheWrite  = 1.25
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

type staticModelDefinition struct {
	ID        string
	Name      string
	Input     []InputMode
	Cost      Cost
	Window    int
	MaxTokens int
}

type discoveryCatalogFetch struct {
	ETag    string
	Content []byte
	// NotModified reports that the server returned 304 and Content should come from cache.
	NotModified bool
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

	models = append(models, discoveredOpenAIResponsesModels(catalog)...)
	models = append(models, discoveredCodexModels()...)

	return models, nil
}

func fetchDiscoveryCatalog(ctx context.Context, options DiscoveryOptions) ([]byte, error) {
	fetched, err := fetchDiscoveryCatalogConditional(ctx, options, "")
	if err != nil {
		return nil, err
	}

	if fetched.NotModified {
		return nil, oops.In("model").Code("model_discovery_not_modified").Errorf(
			"fetch model discovery catalog: unexpected not modified response",
		)
	}

	return fetched.Content, nil
}

func fetchDiscoveryCatalogConditional(
	ctx context.Context,
	options DiscoveryOptions,
	cachedETag string,
) (discoveryCatalogFetch, error) {
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
		return discoveryCatalogFetch{}, oops.In("model").Code("model_discovery_request").Wrapf(
			err,
			"create model discovery request",
		)
	}

	request.Header.Set("Accept", "application/json")
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("User-Agent", "librecode model discovery")
	xetag.Request(request, normalizeDiscoveryETag(cachedETag))

	response, err := client.Do(request)
	if err != nil {
		return discoveryCatalogFetch{}, oops.In("model").Code("model_discovery_fetch").Wrapf(
			err,
			"fetch model discovery catalog",
		)
	}
	defer closeResponseBody(response.Body)

	if response.StatusCode == http.StatusNotModified {
		return discoveryCatalogFetch{
			Content:     nil,
			ETag:        normalizeDiscoveryETag(response.Header.Get("ETag")),
			NotModified: true,
		}, nil
	}

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return discoveryCatalogFetch{}, oops.In("model").Code("model_discovery_status").Errorf(
			"fetch model discovery catalog: unexpected status %s",
			response.Status,
		)
	}

	content, err := io.ReadAll(io.LimitReader(response.Body, discoveryCatalogMaxBytes))
	if err != nil {
		return discoveryCatalogFetch{}, oops.In("model").Code("model_discovery_read").Wrapf(
			err,
			"read model discovery catalog",
		)
	}

	return discoveryCatalogFetch{
		Content:     content,
		ETag:        normalizeDiscoveryETag(response.Header.Get("ETag")),
		NotModified: false,
	}, nil
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
		if !definition.ToolCall || openAIUnsupportedDiscoveredModel(mapping.Provider, modelID, &definition) {
			continue
		}

		models = append(models, modelFromDiscovery(modelID, &definition, mapping))
	}

	return models
}

func openAIUnsupportedDiscoveredModel(provider, fallbackModelID string, definition *discoveryModel) bool {
	if provider != providerOpenAI {
		return false
	}

	modelID := fallbackModelID
	if definition.ID != "" {
		modelID = definition.ID
	}

	return modelID == "gpt-5.6"
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

	model := Model{
		ThinkingLevelMap: thinkingLevelsForModel(mapping.Provider, discoveryModelID),
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
	patchOpenAIResponsesDiscoveredModel(&model)

	return model
}

func patchOpenAIResponsesDiscoveredModel(discovered *Model) {
	if discovered.Provider != providerOpenAI || !openAIResponsesShortContextModel(discovered.ID) {
		return
	}

	discovered.ContextWindow = openAIResponsesContextWindow
	if discovered.MaxTokens == 0 {
		discovered.MaxTokens = largeMaxOutputTokens
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

func discoveredOpenAIResponsesModels(catalog map[string]discoveryProvider) []Model {
	openAIProvider, ok := catalog["openai"]
	if !ok {
		return []Model{}
	}

	metadata := openAIResponsesMetadata()
	definitions := discoveredOpenAIResponsesDefinitions()
	models := make([]Model, 0, len(definitions))

	for index := range definitions {
		definition := definitions[index]
		if _, ok := openAIProvider.Models[definition.ID]; ok {
			continue
		}

		models = append(models, modelFromStaticDefinition(providerOpenAI, &metadata, &definition))
	}

	return models
}

func discoveredCodexModels() []Model {
	metadata := openAICodexMetadata()
	definitions := discoveredCodexDefinitions()

	models := make([]Model, 0, len(definitions))
	for index := range definitions {
		models = append(models, modelFromStaticDefinition(providerOpenAICodex, &metadata, &definitions[index]))
	}

	return models
}

func modelFromStaticDefinition(
	provider string,
	metadata *providerMetadata,
	definition *staticModelDefinition,
) Model {
	maxTokens := definition.MaxTokens
	if maxTokens == 0 {
		maxTokens = metadata.MaxTokens
	}

	return Model{
		ThinkingLevelMap: thinkingLevelsForModel(provider, definition.ID),
		Headers:          cloneStringMap(metadata.Headers),
		Compat:           cloneAnyMap(metadata.Compat),
		Provider:         provider,
		ID:               definition.ID,
		Name:             definition.Name,
		API:              metadata.API,
		BaseURL:          metadata.BaseURL,
		Input:            append([]InputMode{}, definition.Input...),
		Cost:             definition.Cost,
		ContextWindow:    definition.Window,
		MaxTokens:        maxTokens,
		Reasoning:        metadata.Reasoning,
	}
}

const discoveryCatalogMaxBytes = 16 * units.MiB

func codexGPT52Cost() Cost {
	return Cost{Input: gpt52InputCost, Output: gpt52OutputCost, CacheRead: gpt52CacheCost, CacheWrite: 0}
}

func codexGPT54Cost() Cost {
	return Cost{Input: gpt54InputCost, Output: gpt54OutputCost, CacheRead: gpt54CacheCost, CacheWrite: 0}
}

func codexGPT54MiniCost() Cost {
	return Cost{Input: gpt54MiniInput, Output: gpt54MiniOutput, CacheRead: gpt54MiniCache, CacheWrite: 0}
}

func codexGPT55Cost() Cost {
	return Cost{Input: gpt55InputCost, Output: gpt55OutputCost, CacheRead: gpt55CacheCost, CacheWrite: 0}
}

func gpt56SolCost() Cost {
	return Cost{
		Input:      gpt56SolInputCost,
		Output:     gpt56SolOutputCost,
		CacheRead:  gpt56SolCacheCost,
		CacheWrite: gpt56SolCacheWrite,
	}
}

func gpt56TerraCost() Cost {
	return Cost{
		Input:      gpt56TerraInputCost,
		Output:     gpt56TerraOutputCost,
		CacheRead:  gpt56TerraCacheCost,
		CacheWrite: gpt56TerraCacheWrite,
	}
}

func gpt56LunaCost() Cost {
	return Cost{
		Input:      gpt56LunaInputCost,
		Output:     gpt56LunaOutputCost,
		CacheRead:  gpt56LunaCacheCost,
		CacheWrite: gpt56LunaCacheWrite,
	}
}

func discoveredOpenAIResponsesDefinitions() []staticModelDefinition {
	return gpt56ModelDefinitions(openAIResponsesContextWindow, largeMaxOutputTokens)
}

func gpt56ModelDefinitions(window, maxTokens int) []staticModelDefinition {
	textImage := []InputMode{InputText, InputImage}

	return []staticModelDefinition{
		newModelDefinition(gpt56Sol, "GPT-5.6 Sol", textImage, window, maxTokens, gpt56SolCost()),
		newModelDefinition(gpt56Terra, "GPT-5.6 Terra", textImage, window, maxTokens, gpt56TerraCost()),
		newModelDefinition(gpt56Luna, "GPT-5.6 Luna", textImage, window, maxTokens, gpt56LunaCost()),
	}
}

const codexExplicitModelCount = 9

func discoveredCodexDefinitions() []staticModelDefinition {
	textImage := []InputMode{InputText, InputImage}
	definitions := make([]staticModelDefinition, 0, codexExplicitModelCount)
	definitions = append(definitions,
		newModelDefinition(gpt52, "GPT-5.2", textImage, openAICodexContextWindow, 0, codexGPT52Cost()),
		newModelDefinition(
			"gpt-5.3-codex",
			"GPT-5.3 Codex",
			textImage,
			openAICodexContextWindow,
			0,
			codexGPT52Cost(),
		),
		newModelDefinition(
			"gpt-5.3-codex-spark",
			"GPT-5.3 Codex Spark",
			[]InputMode{InputText},
			openAICodexSparkContextWindow,
			0,
			codexGPT52Cost(),
		),
		newModelDefinition(gpt54, "GPT-5.4", textImage, openAICodexContextWindow, 0, codexGPT54Cost()),
		newModelDefinition(
			"gpt-5.4-mini",
			"GPT-5.4 mini",
			textImage,
			openAICodexContextWindow,
			0,
			codexGPT54MiniCost(),
		),
		newModelDefinition(gpt55, "GPT-5.5", textImage, openAICodexContextWindow, 0, codexGPT55Cost()),
	)

	return append(definitions, gpt56ModelDefinitions(openAICodexGPT56ContextWindow, 0)...)
}

func newModelDefinition(
	modelID string,
	name string,
	input []InputMode,
	window int,
	maxTokens int,
	cost Cost,
) staticModelDefinition {
	return staticModelDefinition{
		ID:        modelID,
		Name:      name,
		Input:     append([]InputMode{}, input...),
		Cost:      cost,
		Window:    window,
		MaxTokens: maxTokens,
	}
}

func thinkingLevelsForModel(provider, modelID string) map[ThinkingLevel]*string {
	levels := map[ThinkingLevel]*string{}
	addAnthropicThinkingLevels(levels, provider, modelID)
	addOpenAIThinkingOff(levels, provider, modelID)
	addOpenAIThinkingNone(levels, provider, modelID)
	addOpenAIXHigh(levels, modelID)
	addOpenAIMax(levels, modelID)
	addOpenAIMinimal(levels, provider, modelID)
	addZAIThinkingLevels(levels, provider, modelID)

	if len(levels) == 0 {
		return nil
	}

	return levels
}

func addAnthropicThinkingLevels(levels map[ThinkingLevel]*string, provider, modelID string) {
	if provider != providerAnthropic && provider != providerAnthropicClaude {
		return
	}

	levels[ThinkingOff] = nil

	if !anthropicmodel.SupportsXHigh(modelID) {
		return
	}

	xhigh := string(ThinkingXHigh)
	levels[ThinkingXHigh] = &xhigh
}

func addOpenAIThinkingOff(levels map[ThinkingLevel]*string, provider, modelID string) {
	if !openAIProviderSupportsThinkingOff(provider) || !strings.HasPrefix(modelID, "gpt-5") {
		return
	}

	levels[ThinkingOff] = nil
}

func openAIProviderSupportsThinkingOff(provider string) bool {
	return provider == providerOpenAI || provider == providerAzureOpenAIResponses || provider == providerOpenAICodex
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

	xhigh := string(ThinkingXHigh)
	levels[ThinkingXHigh] = &xhigh
}

func addOpenAIMax(levels map[ThinkingLevel]*string, modelID string) {
	if !openAISupportsMax(modelID) {
		return
	}

	maxEffort := string(ThinkingMax)
	levels[ThinkingMax] = &maxEffort
}

func addOpenAIMinimal(levels map[ThinkingLevel]*string, provider, modelID string) {
	if provider != providerOpenAICodex || !openAICodexMapsMinimalToLow(modelID) {
		return
	}

	low := string(ThinkingLow)
	levels[ThinkingMinimal] = &low
}

func addZAIThinkingLevels(levels map[ThinkingLevel]*string, provider, modelID string) {
	if provider != providerZAI || !strings.HasPrefix(modelID, glm52) {
		return
	}

	maps.Copy(levels, zaiThinkingLevelMap())
}

func openAISupportsXHigh(modelID string) bool {
	return strings.Contains(modelID, gpt52) ||
		strings.Contains(modelID, "gpt-5.3") ||
		strings.Contains(modelID, "gpt-5.4") ||
		strings.Contains(modelID, gpt55) ||
		strings.Contains(modelID, "gpt-5.6")
}

func openAISupportsMax(modelID string) bool {
	return strings.Contains(modelID, "gpt-5.6")
}

func openAICodexMapsMinimalToLow(modelID string) bool {
	return strings.HasPrefix(modelID, "gpt-5.")
}

func closeResponseBody(body io.Closer) {
	if err := body.Close(); err != nil {
		_ = err
	}
}

func openAIResponsesNoReasoningModel(modelID string) bool {
	switch modelID {
	case "gpt-5.1", gpt52, "gpt-5.3-codex", gpt54, "gpt-5.4-mini", "gpt-5.4-nano", gpt55,
		gpt56Sol, gpt56Terra, gpt56Luna:
		return true
	default:
		return false
	}
}

func openAIResponsesShortContextModel(modelID string) bool {
	switch modelID {
	case gpt54, gpt55, gpt56Sol, gpt56Terra, gpt56Luna:
		return true
	default:
		return false
	}
}
