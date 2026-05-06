package model

import (
	"path"
	"sort"
	"strings"
)

// ScopedModel is a model with an optional thinking-level override.
type ScopedModel struct {
	ThinkingLevel *ThinkingLevel `json:"thinking_level,omitempty"`
	Model         Model          `json:"model"`
}

// ParsedPattern describes model pattern parsing.
type ParsedPattern struct {
	ThinkingLevel *ThinkingLevel `json:"thinking_level,omitempty"`
	Model         *Model         `json:"model,omitempty"`
	Warning       string         `json:"warning,omitempty"`
}

// ResolveCLIModelOptions contains CLI model flags.
type ResolveCLIModelOptions struct {
	Registry    *Registry `json:"-"`
	CLIProvider string    `json:"cli_provider,omitempty"`
	CLIModel    string    `json:"cli_model,omitempty"`
}

// ResolveCLIModelResult describes CLI model resolution.
type ResolveCLIModelResult struct {
	ThinkingLevel *ThinkingLevel `json:"thinking_level,omitempty"`
	Model         *Model         `json:"model,omitempty"`
	Warning       string         `json:"warning,omitempty"`
	Error         string         `json:"error,omitempty"`
}

// FindExactModelReferenceMatch resolves a bare ID or provider/model reference.
func FindExactModelReferenceMatch(modelReference string, availableModels []Model) (Model, bool) {
	trimmedReference := strings.TrimSpace(modelReference)
	if trimmedReference == "" {
		return zeroModel(), false
	}
	if model, found := exactlyOneModel(canonicalReferenceMatches(trimmedReference, availableModels)); found {
		return model, true
	}
	if provider, modelID, ok := providerModelReference(trimmedReference); ok {
		matches := providerReferenceMatches(provider, modelID, availableModels)
		if model, found := exactlyOneModel(matches); found {
			return model, true
		}
	}

	return exactlyOneModel(idReferenceMatches(trimmedReference, availableModels))
}

// ParseModelPattern resolves a model pattern and optional :thinking suffix.
func ParseModelPattern(pattern string, availableModels []Model, allowInvalidThinkingFallback bool) ParsedPattern {
	if model, ok := tryMatchModel(pattern, availableModels); ok {
		return ParsedPattern{ThinkingLevel: nil, Model: &model, Warning: ""}
	}
	colonIndex := strings.LastIndex(pattern, ":")
	if colonIndex == -1 {
		return ParsedPattern{ThinkingLevel: nil, Model: nil, Warning: ""}
	}
	prefix := pattern[:colonIndex]
	suffix := pattern[colonIndex+1:]
	if IsValidThinkingLevel(suffix) {
		return parseValidThinkingSuffix(prefix, ThinkingLevel(suffix), availableModels, allowInvalidThinkingFallback)
	}
	if !allowInvalidThinkingFallback {
		return ParsedPattern{ThinkingLevel: nil, Model: nil, Warning: ""}
	}

	return parseInvalidThinkingSuffix(pattern, prefix, suffix, availableModels, allowInvalidThinkingFallback)
}

// ResolveModelScope resolves patterns into unique scoped models and warnings.
func ResolveModelScope(patterns []string, registry *Registry) (scopedModels []ScopedModel, warnings []string) {
	availableModels := registry.Available()
	for _, pattern := range patterns {
		matchedModels, thinkingLevel, patternWarnings := resolveScopePattern(pattern, availableModels)
		warnings = append(warnings, patternWarnings...)
		for matchedIndex := range matchedModels {
			matchedModel := &matchedModels[matchedIndex]
			if !containsScopedModel(scopedModels, matchedModel) {
				scopedModels = append(scopedModels, ScopedModel{ThinkingLevel: thinkingLevel, Model: *matchedModel})
			}
		}
	}

	return scopedModels, warnings
}

// ResolveCLIModel resolves --provider and --model flags against all models.
func ResolveCLIModel(options ResolveCLIModelOptions) ResolveCLIModelResult {
	if options.CLIModel == "" {
		return ResolveCLIModelResult{ThinkingLevel: nil, Model: nil, Warning: "", Error: ""}
	}
	availableModels := options.Registry.All()
	if len(availableModels) == 0 {
		return ResolveCLIModelResult{
			ThinkingLevel: nil,
			Model:         nil,
			Warning:       "",
			Error:         "No models available. Check your installation or add models to models.json.",
		}
	}
	provider, pattern, errMessage := cliProviderAndPattern(options, availableModels)
	if errMessage != "" {
		return ResolveCLIModelResult{ThinkingLevel: nil, Model: nil, Warning: "", Error: errMessage}
	}
	parsed := ParseModelPattern(pattern, cliCandidates(provider, availableModels), false)
	if parsed.Model == nil {
		return ResolveCLIModelResult{
			ThinkingLevel: nil,
			Model:         nil,
			Warning:       "",
			Error:         "No models match \"" + options.CLIModel + "\".",
		}
	}

	return ResolveCLIModelResult{
		ThinkingLevel: parsed.ThinkingLevel,
		Model:         parsed.Model,
		Warning:       parsed.Warning,
		Error:         "",
	}
}

func parseValidThinkingSuffix(
	prefix string,
	thinkingLevel ThinkingLevel,
	availableModels []Model,
	allowInvalidThinkingFallback bool,
) ParsedPattern {
	parsed := ParseModelPattern(prefix, availableModels, allowInvalidThinkingFallback)
	if parsed.Model == nil || parsed.Warning != "" {
		return parsed
	}
	parsed.ThinkingLevel = &thinkingLevel

	return parsed
}

func parseInvalidThinkingSuffix(
	pattern string,
	prefix string,
	suffix string,
	availableModels []Model,
	allowInvalidThinkingFallback bool,
) ParsedPattern {
	parsed := ParseModelPattern(prefix, availableModels, allowInvalidThinkingFallback)
	if parsed.Model == nil {
		return parsed
	}
	parsed.Warning = "Invalid thinking level \"" + suffix + "\" in pattern \"" + pattern + "\". Using default instead."

	return parsed
}

func resolveScopePattern(pattern string, availableModels []Model) ([]Model, *ThinkingLevel, []string) {
	if hasGlob(pattern) {
		return resolveGlobScopePattern(pattern, availableModels)
	}
	parsed := ParseModelPattern(pattern, availableModels, true)
	if parsed.Model == nil {
		return []Model{}, nil, []string{"No models match pattern \"" + pattern + "\""}
	}
	warnings := []string{}
	if parsed.Warning != "" {
		warnings = append(warnings, parsed.Warning)
	}

	return []Model{*parsed.Model}, parsed.ThinkingLevel, warnings
}

func resolveGlobScopePattern(pattern string, availableModels []Model) ([]Model, *ThinkingLevel, []string) {
	globPattern := pattern
	var thinkingLevel *ThinkingLevel
	if prefix, suffix, ok := splitThinkingSuffix(pattern); ok {
		globPattern = prefix
		thinkingLevel = &suffix
	}
	matches := filterModels(availableModels, func(model *Model) bool {
		return globMatches(globPattern, model.Provider+"/"+model.ID) || globMatches(globPattern, model.ID)
	})
	if len(matches) == 0 {
		return []Model{}, nil, []string{"No models match pattern \"" + pattern + "\""}
	}

	return matches, thinkingLevel, []string{}
}

func splitThinkingSuffix(pattern string) (prefix string, thinkingLevel ThinkingLevel, found bool) {
	colonIndex := strings.LastIndex(pattern, ":")
	if colonIndex == -1 {
		return "", "", false
	}
	suffix := pattern[colonIndex+1:]
	if !IsValidThinkingLevel(suffix) {
		return "", "", false
	}

	return pattern[:colonIndex], ThinkingLevel(suffix), true
}

func cliProviderAndPattern(
	options ResolveCLIModelOptions,
	availableModels []Model,
) (provider, pattern, errorMessage string) {
	providerMap := providerLookup(availableModels)
	if options.CLIProvider != "" {
		canonicalProvider, ok := providerMap[strings.ToLower(options.CLIProvider)]
		if !ok {
			return "", "", "Unknown provider \"" + options.CLIProvider + "\"."
		}
		provider = canonicalProvider
	}
	pattern = options.CLIModel
	if provider == "" {
		provider, pattern = inferProvider(pattern, providerMap)
	}

	return provider, pattern, ""
}

func inferProvider(pattern string, providerMap map[string]string) (provider, modelPattern string) {
	providerPrefix, modelID, ok := strings.Cut(pattern, "/")
	if !ok {
		return "", pattern
	}
	canonicalProvider, found := providerMap[strings.ToLower(providerPrefix)]
	if !found {
		return "", pattern
	}

	return canonicalProvider, modelID
}

func cliCandidates(provider string, availableModels []Model) []Model {
	if provider == "" {
		return availableModels
	}

	return filterModels(availableModels, func(model *Model) bool {
		return strings.EqualFold(model.Provider, provider)
	})
}

func providerLookup(models []Model) map[string]string {
	providers := map[string]string{}
	for index := range models {
		model := &models[index]
		providers[strings.ToLower(model.Provider)] = model.Provider
	}

	return providers
}

func tryMatchModel(modelPattern string, availableModels []Model) (Model, bool) {
	if exactMatch, ok := FindExactModelReferenceMatch(modelPattern, availableModels); ok {
		return exactMatch, true
	}
	matches := filterModels(availableModels, func(model *Model) bool {
		return strings.Contains(strings.ToLower(model.ID), strings.ToLower(modelPattern)) ||
			strings.Contains(strings.ToLower(model.Name), strings.ToLower(modelPattern))
	})
	if len(matches) == 0 {
		return zeroModel(), false
	}
	aliases := filterModels(matches, func(model *Model) bool { return isAlias(model.ID) })
	if len(aliases) > 0 {
		sort.Slice(aliases, func(leftIndex, rightIndex int) bool {
			return aliases[leftIndex].ID > aliases[rightIndex].ID
		})

		return aliases[0], true
	}
	sort.Slice(matches, func(leftIndex, rightIndex int) bool {
		return matches[leftIndex].ID > matches[rightIndex].ID
	})

	return matches[0], true
}

func canonicalReferenceMatches(reference string, availableModels []Model) []Model {
	return filterModels(availableModels, func(model *Model) bool {
		return strings.EqualFold(model.Provider+"/"+model.ID, reference)
	})
}

func providerReferenceMatches(provider, modelID string, availableModels []Model) []Model {
	return filterModels(availableModels, func(model *Model) bool {
		return strings.EqualFold(model.Provider, provider) && strings.EqualFold(model.ID, modelID)
	})
}

func idReferenceMatches(reference string, availableModels []Model) []Model {
	return filterModels(availableModels, func(model *Model) bool {
		return strings.EqualFold(model.ID, reference)
	})
}

func exactlyOneModel(matches []Model) (Model, bool) {
	if len(matches) != 1 {
		return zeroModel(), false
	}

	return matches[0], true
}

func providerModelReference(reference string) (provider, modelID string, found bool) {
	provider, modelID, found = strings.Cut(reference, "/")

	return provider, modelID, found && provider != "" && modelID != ""
}

func isAlias(modelID string) bool {
	if strings.HasSuffix(modelID, "-latest") {
		return true
	}
	if len(modelID) < 9 {
		return true
	}
	dateSuffix := modelID[len(modelID)-9:]
	if dateSuffix[0] != '-' {
		return true
	}
	for _, character := range dateSuffix[1:] {
		if character < '0' || character > '9' {
			return true
		}
	}

	return false
}

func hasGlob(pattern string) bool {
	return strings.ContainsAny(pattern, "*?[")
}

func globMatches(pattern, value string) bool {
	matched, err := path.Match(strings.ToLower(pattern), strings.ToLower(value))

	return err == nil && matched
}

func containsScopedModel(scopedModels []ScopedModel, model *Model) bool {
	for index := range scopedModels {
		if ModelsAreEqual(&scopedModels[index].Model, model) {
			return true
		}
	}

	return false
}

func filterModels(models []Model, predicate func(model *Model) bool) []Model {
	filtered := make([]Model, 0, len(models))
	for index := range models {
		model := &models[index]
		if predicate(model) {
			filtered = append(filtered, *model)
		}
	}

	return filtered
}
