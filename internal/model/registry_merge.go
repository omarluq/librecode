package model

import "maps"

func applyProviderPatches(
	models []Model,
	patches map[string]providerPatch,
	overrides map[string]map[string]modelOverride,
) []Model {
	return loMapModels(models, func(model Model) Model {
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
	maps.Copy(merged, right)

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
	maps.Copy(merged, right)

	return merged
}

func mergeThinkingMaps(left, right map[ThinkingLevel]*string) map[ThinkingLevel]*string {
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

func loMapModels(models []Model, mapper func(Model) Model) []Model {
	mapped := make([]Model, 0, len(models))
	for index := range models {
		mapped = append(mapped, mapper(models[index]))
	}

	return mapped
}
