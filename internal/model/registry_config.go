package model

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/samber/lo"
	"github.com/samber/oops"
)

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
		return emptyCustomModelsResult(oops.In("model").Code("models_missing_providers").Errorf(
			"models.json %s must contain providers",
			sourcePath,
		))
	}

	result := emptyCustomModelsResult(nil)
	for providerName, providerCfg := range config.Providers {
		if providerName == "" {
			result.Err = oops.In("model").Code("models_empty_provider_name").Errorf(
				"models.json %s contains an empty provider name",
				sourcePath,
			)
			continue
		}
		result.ProviderConfigs[providerName] = requestConfig(&providerCfg)
		if providerCfg.BaseURL != "" || providerCfg.Compat != nil {
			result.ProviderPatches[providerName] = providerPatch{
				Compat:  cloneAnyMap(providerCfg.Compat),
				BaseURL: providerCfg.BaseURL,
			}
		}
		for modelID, override := range providerCfg.ModelOverrides {
			storeModelOverride(result.ModelOverrides, providerName, modelID, &override)
		}
		result.Models = append(result.Models, modelsFromProvider(providerName, &providerCfg)...)
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
