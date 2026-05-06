package model

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/samber/lo"
	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/auth"
)

// ConfigSource reads model registry configuration from database-backed runtime documents.
type ConfigSource interface {
	Read() (content []byte, found bool, err error)
}

// RegistryOptions configures a model registry.
type RegistryOptions struct {
	ConfigSource ConfigSource  `json:"-"`
	Auth         *auth.Storage `json:"-"`
	ModelsPath   string        `json:"models_path"`
	BuiltIns     []Model       `json:"built_ins"`
}

// Registry loads built-in and custom models and resolves provider request auth.
type Registry struct {
	loadError       error
	configSource    ConfigSource
	auth            *auth.Storage
	providerConfigs map[string]providerRequestConfig
	modelsPath      string
	models          []Model
	builtIns        []Model
	lock            sync.RWMutex
}

type providerRequestConfig struct {
	Headers    map[string]string
	APIKey     string
	AuthHeader bool
}

type modelsConfig struct {
	Providers map[string]providerConfig `json:"providers"`
}

type providerConfig struct {
	ModelOverrides map[string]modelOverride `json:"modelOverrides"`
	Headers        map[string]string        `json:"headers"`
	Compat         map[string]any           `json:"compat"`
	AuthHeader     *bool                    `json:"authHeader"`
	Name           string                   `json:"name"`
	BaseURL        string                   `json:"baseUrl"`
	APIKey         string                   `json:"apiKey"`
	API            string                   `json:"api"`
	Models         []modelDefinition        `json:"models"`
}

type modelDefinition struct {
	ThinkingLevelMap map[ThinkingLevel]*string `json:"thinkingLevelMap"`
	Headers          map[string]string         `json:"headers"`
	Compat           map[string]any            `json:"compat"`
	Cost             *Cost                     `json:"cost"`
	Reasoning        *bool                     `json:"reasoning"`
	Name             string                    `json:"name"`
	ID               string                    `json:"id"`
	API              string                    `json:"api"`
	BaseURL          string                    `json:"baseUrl"`
	Input            []InputMode               `json:"input"`
	ContextWindow    int                       `json:"contextWindow"`
	MaxTokens        int                       `json:"maxTokens"`
}

type modelOverride struct {
	ThinkingLevelMap map[ThinkingLevel]*string `json:"thinkingLevelMap"`
	Headers          map[string]string         `json:"headers"`
	Compat           map[string]any            `json:"compat"`
	Cost             *partialCost              `json:"cost"`
	Name             *string                   `json:"name"`
	ContextWindow    *int                      `json:"contextWindow"`
	MaxTokens        *int                      `json:"maxTokens"`
	Reasoning        *bool                     `json:"reasoning"`
	Input            []InputMode               `json:"input"`
}

type partialCost struct {
	Input      *float64 `json:"input"`
	Output     *float64 `json:"output"`
	CacheRead  *float64 `json:"cacheRead"`
	CacheWrite *float64 `json:"cacheWrite"`
}

type customModelsResult struct {
	Err             error
	ProviderConfigs map[string]providerRequestConfig
	ModelOverrides  map[string]map[string]modelOverride
	ProviderPatches map[string]providerPatch
	Models          []Model
}

type providerPatch struct {
	Compat  map[string]any
	BaseURL string
}

// NewRegistry creates and refreshes a registry.
func NewRegistry(options *RegistryOptions) *Registry {
	resolvedOptions := registryOptions(options)
	registry := &Registry{
		configSource:    resolvedOptions.ConfigSource,
		auth:            resolvedOptions.Auth,
		providerConfigs: map[string]providerRequestConfig{},
		modelsPath:      resolvedOptions.ModelsPath,
		models:          []Model{},
		builtIns:        cloneModels(resolvedOptions.BuiltIns),
		lock:            sync.RWMutex{},
		loadError:       nil,
	}
	registry.Refresh()

	return registry
}

// Refresh reloads models from disk and registered built-ins.
func (registry *Registry) Refresh() {
	customResult := registry.loadCustomModels()
	builtIns := applyProviderPatches(registry.builtIns, customResult.ProviderPatches, customResult.ModelOverrides)
	models := mergeCustomModels(builtIns, customResult.Models)

	registry.lock.Lock()
	registry.models = models
	registry.providerConfigs = customResult.ProviderConfigs
	registry.loadError = customResult.Err
	registry.lock.Unlock()
}

// Error returns the latest models.json load error.
func (registry *Registry) Error() error {
	registry.lock.RLock()
	defer registry.lock.RUnlock()

	return registry.loadError
}

// All returns all known models.
func (registry *Registry) All() []Model {
	registry.lock.RLock()
	defer registry.lock.RUnlock()

	return cloneModels(registry.models)
}

// Available returns models whose provider has some configured auth.
func (registry *Registry) Available() []Model {
	models := registry.All()

	return lo.Filter(models, func(model Model, _ int) bool {
		return registry.HasAuth(model.Provider)
	})
}

// HasAuth reports whether provider auth can be resolved.
func (registry *Registry) HasAuth(provider string) bool {
	if registry.auth != nil && registry.auth.HasAuth(provider) {
		return true
	}
	registry.lock.RLock()
	defer registry.lock.RUnlock()

	return registry.providerConfigs[provider].APIKey != ""
}

// RequestAuth returns auth and headers for a model request.
func (registry *Registry) RequestAuth(provider string) RequestAuth {
	registry.lock.RLock()
	config := registry.providerConfigs[provider]
	registry.lock.RUnlock()

	apiKey := config.APIKey
	if registry.auth != nil {
		if resolvedAPIKey, ok := registry.auth.APIKey(provider); ok {
			apiKey = resolvedAPIKey
		}
	}
	if apiKey == "" && config.AuthHeader {
		return RequestAuth{Headers: cloneStringMap(config.Headers), APIKey: "", Error: "missing API key", OK: false}
	}

	return RequestAuth{Headers: cloneStringMap(config.Headers), APIKey: apiKey, Error: "", OK: true}
}

func registryOptions(options *RegistryOptions) *RegistryOptions {
	if options != nil {
		if len(options.BuiltIns) == 0 {
			copyOptions := *options
			copyOptions.BuiltIns = BuiltInModels()
			return &copyOptions
		}

		return options
	}

	return &RegistryOptions{ConfigSource: nil, Auth: nil, ModelsPath: "", BuiltIns: BuiltInModels()}
}

func (registry *Registry) loadCustomModels() customModelsResult {
	if registry.configSource != nil {
		content, found, err := registry.configSource.Read()
		if err != nil || !found {
			return emptyCustomModelsResult(err)
		}

		return parseCustomModels(content, "database:models")
	}
	if registry.modelsPath == "" {
		return emptyCustomModelsResult(nil)
	}
	content, err := readModelFile(registry.modelsPath)
	if errorsIsNotExist(err) {
		return emptyCustomModelsResult(nil)
	}
	if err != nil {
		return emptyCustomModelsResult(err)
	}

	return parseCustomModels(content, registry.modelsPath)
}

func parseCustomModels(content []byte, sourcePath string) customModelsResult {
	var config modelsConfig
	if err := json.Unmarshal([]byte(stripJSONComments(string(content))), &config); err != nil {
		return emptyCustomModelsResult(oops.In("model").Code("parse_models").Wrapf(err, "parse models.json"))
	}
	if config.Providers == nil {
		return emptyCustomModelsResult(fmt.Errorf("models.json %s must contain providers", sourcePath))
	}

	result := emptyCustomModelsResult(nil)
	for providerName, providerConfig := range config.Providers {
		if providerName == "" {
			result.Err = fmt.Errorf("models.json %s contains an empty provider name", sourcePath)
			continue
		}
		result.ProviderConfigs[providerName] = requestConfig(&providerConfig)
		if providerConfig.BaseURL != "" || providerConfig.Compat != nil {
			result.ProviderPatches[providerName] = providerPatch{
				Compat:  cloneAnyMap(providerConfig.Compat),
				BaseURL: providerConfig.BaseURL,
			}
		}
		for modelID, override := range providerConfig.ModelOverrides {
			storeModelOverride(result.ModelOverrides, providerName, modelID, &override)
		}
		result.Models = append(result.Models, modelsFromProvider(providerName, &providerConfig)...)
	}

	return result
}

func requestConfig(config *providerConfig) providerRequestConfig {
	authHeader := true
	if config.AuthHeader != nil {
		authHeader = *config.AuthHeader
	}

	return providerRequestConfig{
		Headers:    cloneStringMap(config.Headers),
		APIKey:     config.APIKey,
		AuthHeader: authHeader,
	}
}

func modelsFromProvider(providerName string, config *providerConfig) []Model {
	return lo.FilterMap(config.Models, func(definition modelDefinition, _ int) (Model, bool) {
		if definition.ID == "" {
			return zeroModel(), false
		}

		return modelFromDefinition(providerName, config, &definition), true
	})
}

func modelFromDefinition(providerName string, config *providerConfig, definition *modelDefinition) Model {
	name := definition.Name
	if name == "" {
		name = definition.ID
	}
	api := definition.API
	if api == "" {
		api = config.API
	}
	baseURL := definition.BaseURL
	if baseURL == "" {
		baseURL = config.BaseURL
	}
	input := definition.Input
	if len(input) == 0 {
		input = []InputMode{InputText}
	}
	reasoning := false
	if definition.Reasoning != nil {
		reasoning = *definition.Reasoning
	}
	cost := Cost{Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0}
	if definition.Cost != nil {
		cost = *definition.Cost
	}

	return Model{
		ThinkingLevelMap: cloneThinkingMap(definition.ThinkingLevelMap),
		Headers:          cloneStringMap(definition.Headers),
		Compat:           cloneAnyMap(definition.Compat),
		Provider:         providerName,
		ID:               definition.ID,
		Name:             name,
		API:              api,
		BaseURL:          baseURL,
		Input:            append([]InputMode{}, input...),
		Cost:             cost,
		ContextWindow:    definition.ContextWindow,
		MaxTokens:        definition.MaxTokens,
		Reasoning:        reasoning,
	}
}

func applyProviderPatches(
	models []Model,
	patches map[string]providerPatch,
	overrides map[string]map[string]modelOverride,
) []Model {
	return lo.Map(models, func(model Model, _ int) Model {
		patched := cloneModel(&model)
		if patch, ok := patches[patched.Provider]; ok {
			patched.BaseURL = firstNonEmpty(patch.BaseURL, patched.BaseURL)
			patched.Compat = mergeAnyMaps(patched.Compat, patch.Compat)
		}
		if providerOverrides, ok := overrides[patched.Provider]; ok {
			if override, ok := providerOverrides[patched.ID]; ok {
				patched = applyModelOverride(&patched, &override)
			}
		}

		return patched
	})
}

func applyModelOverride(model *Model, override *modelOverride) Model {
	result := cloneModel(model)
	if override.Name != nil {
		result.Name = *override.Name
	}
	if override.Reasoning != nil {
		result.Reasoning = *override.Reasoning
	}
	if override.ThinkingLevelMap != nil {
		result.ThinkingLevelMap = mergeThinkingMaps(result.ThinkingLevelMap, override.ThinkingLevelMap)
	}
	if len(override.Input) > 0 {
		result.Input = append([]InputMode{}, override.Input...)
	}
	if override.ContextWindow != nil {
		result.ContextWindow = *override.ContextWindow
	}
	if override.MaxTokens != nil {
		result.MaxTokens = *override.MaxTokens
	}
	if override.Cost != nil {
		result.Cost = mergeCost(result.Cost, override.Cost)
	}
	result.Headers = mergeStringMaps(result.Headers, override.Headers)
	result.Compat = mergeAnyMaps(result.Compat, override.Compat)

	return result
}

func mergeCustomModels(builtIns, customModels []Model) []Model {
	merged := cloneModels(builtIns)
	for customIndex := range customModels {
		customModel := &customModels[customIndex]
		inserted := false
		for mergedIndex := range merged {
			existing := &merged[mergedIndex]
			if ModelsAreEqual(existing, customModel) {
				merged[mergedIndex] = cloneModel(customModel)
				inserted = true
				break
			}
		}
		if !inserted {
			merged = append(merged, cloneModel(customModel))
		}
	}

	return merged
}

func storeModelOverride(
	overrides map[string]map[string]modelOverride,
	providerName string,
	modelID string,
	override *modelOverride,
) {
	if overrides[providerName] == nil {
		overrides[providerName] = map[string]modelOverride{}
	}
	overrides[providerName][modelID] = *override
}

func emptyCustomModelsResult(err error) customModelsResult {
	return customModelsResult{
		ProviderConfigs: map[string]providerRequestConfig{},
		ModelOverrides:  map[string]map[string]modelOverride{},
		ProviderPatches: map[string]providerPatch{},
		Models:          []Model{},
		Err:             err,
	}
}

func readModelFile(path string) ([]byte, error) {
	cleanPath := filepath.Clean(path)

	return fs.ReadFile(os.DirFS(filepath.Dir(cleanPath)), filepath.Base(cleanPath))
}

func errorsIsNotExist(err error) bool {
	return err != nil && os.IsNotExist(err)
}

func firstNonEmpty(primary, fallback string) string {
	if primary != "" {
		return primary
	}

	return fallback
}

func mergeCost(cost Cost, override *partialCost) Cost {
	if override.Input != nil {
		cost.Input = *override.Input
	}
	if override.Output != nil {
		cost.Output = *override.Output
	}
	if override.CacheRead != nil {
		cost.CacheRead = *override.CacheRead
	}
	if override.CacheWrite != nil {
		cost.CacheWrite = *override.CacheWrite
	}

	return cost
}

func mergeStringMaps(left, right map[string]string) map[string]string {
	if left == nil && right == nil {
		return nil
	}
	merged := cloneStringMap(left)
	if merged == nil {
		merged = map[string]string{}
	}
	for key, value := range right {
		merged[key] = value
	}

	return merged
}

func mergeAnyMaps(left, right map[string]any) map[string]any {
	if left == nil && right == nil {
		return nil
	}
	merged := cloneAnyMap(left)
	if merged == nil {
		merged = map[string]any{}
	}
	for key, value := range right {
		merged[key] = value
	}

	return merged
}

func mergeThinkingMaps(
	left map[ThinkingLevel]*string,
	right map[ThinkingLevel]*string,
) map[ThinkingLevel]*string {
	if left == nil && right == nil {
		return nil
	}
	merged := cloneThinkingMap(left)
	if merged == nil {
		merged = map[ThinkingLevel]*string{}
	}
	for key, value := range right {
		if value == nil {
			merged[key] = nil
			continue
		}
		copied := *value
		merged[key] = &copied
	}

	return merged
}
