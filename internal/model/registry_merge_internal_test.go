package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	mergeTestProvider  = "provider"
	mergeTestHeaderKey = "x-base"
	mergeTestModeKey   = "mode"
	mergeTestBase      = "base"
	mergeTestOverride  = "override"
)

func TestMergeCostAppliesPartialOverrides(t *testing.T) {
	t.Parallel()

	cost := Cost{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4}
	inputCost := 10.0
	cacheReadCost := 30.0
	override := &partialCost{
		Input:      &inputCost,
		Output:     nil,
		CacheRead:  &cacheReadCost,
		CacheWrite: nil,
	}

	merged := mergeCost(cost, override)

	assert.Equal(t, Cost{Input: 10, Output: 2, CacheRead: 30, CacheWrite: 4}, merged)
}

func TestMergeStringMaps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		left     map[string]string
		right    map[string]string
		expected map[string]string
		name     string
	}{
		{name: "both nil", left: nil, right: nil, expected: nil},
		{
			name:     "right overrides left",
			left:     map[string]string{"a": "1", "b": "2"},
			right:    map[string]string{"b": "3"},
			expected: map[string]string{"a": "1", "b": "3"},
		},
		{name: "right only", left: nil, right: map[string]string{"a": "1"}, expected: map[string]string{"a": "1"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			merged := mergeStringMaps(test.left, test.right)

			assert.Equal(t, test.expected, merged)
			if len(test.left) > 0 && len(test.right) > 0 {
				merged["a"] = "changed"
				assert.Equal(t, "1", test.left["a"])
			}
		})
	}
}

func TestMergeAnyMaps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		left     map[string]any
		right    map[string]any
		expected map[string]any
		name     string
	}{
		{name: "both nil", left: nil, right: nil, expected: nil},
		{
			name:     "right overrides left",
			left:     map[string]any{"a": "1", "b": 2},
			right:    map[string]any{"b": 3},
			expected: map[string]any{"a": "1", "b": 3},
		},
		{name: "right only", left: nil, right: map[string]any{"a": true}, expected: map[string]any{"a": true}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			merged := mergeAnyMaps(test.left, test.right)

			assert.Equal(t, test.expected, merged)
		})
	}
}

func TestMergeThinkingMaps(t *testing.T) {
	t.Parallel()

	leftHigh := "high-left"
	rightHigh := "high-right"
	rightLow := "low-right"
	left := map[ThinkingLevel]*string{
		ThinkingHigh: &leftHigh,
		ThinkingLow:  nil,
	}
	right := map[ThinkingLevel]*string{
		ThinkingHigh: &rightHigh,
		ThinkingLow:  &rightLow,
		ThinkingOff:  nil,
	}

	merged := mergeThinkingMaps(left, right)

	require.NotNil(t, merged[ThinkingHigh])
	require.NotNil(t, merged[ThinkingLow])
	assert.Equal(t, "high-right", *merged[ThinkingHigh])
	assert.Equal(t, "low-right", *merged[ThinkingLow])
	assert.Nil(t, merged[ThinkingOff])

	rightHigh = "mutated"
	assert.Equal(t, "high-right", *merged[ThinkingHigh])

	assert.Nil(t, mergeThinkingMaps(nil, nil))
}

func TestApplyModelOverrideMergesOptionalFields(t *testing.T) {
	t.Parallel()

	baseHeader := map[string]string{mergeTestHeaderKey: mergeTestBase}
	baseCompat := map[string]any{mergeTestModeKey: mergeTestBase}
	thinkingValue := "enabled"
	model := mergeTestModel("model", "Base")
	model.Headers = baseHeader
	model.Compat = baseCompat
	model.Cost = Cost{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4}
	model.ThinkingLevelMap = map[ThinkingLevel]*string{ThinkingHigh: &thinkingValue}

	name := "Override"
	contextWindow := 128
	maxTokens := 64
	reasoning := true
	newThinking := "low"
	overrideInputCost := 10.0
	overrideCacheWriteCost := 40.0
	override := &modelOverride{
		ThinkingLevelMap: map[ThinkingLevel]*string{ThinkingLow: &newThinking},
		Headers:          map[string]string{mergeTestHeaderKey: mergeTestOverride, "x-new": "new"},
		Compat:           map[string]any{mergeTestModeKey: mergeTestOverride, "extra": true},
		Cost: &partialCost{
			Input:      &overrideInputCost,
			Output:     nil,
			CacheRead:  nil,
			CacheWrite: &overrideCacheWriteCost,
		},
		Name:          &name,
		ContextWindow: &contextWindow,
		MaxTokens:     &maxTokens,
		Reasoning:     &reasoning,
		Input:         []InputMode{InputText, InputImage},
	}

	result := applyModelOverride(&model, override)

	assert.Equal(t, "Override", result.Name)
	assert.Equal(t, 128, result.ContextWindow)
	assert.Equal(t, 64, result.MaxTokens)
	assert.True(t, result.Reasoning)
	assert.Equal(t, []InputMode{InputText, InputImage}, result.Input)
	assert.Equal(t, map[string]string{mergeTestHeaderKey: mergeTestOverride, "x-new": "new"}, result.Headers)
	assert.Equal(t, map[string]any{mergeTestModeKey: mergeTestOverride, "extra": true}, result.Compat)
	assert.Equal(t, Cost{Input: 10, Output: 2, CacheRead: 3, CacheWrite: 40}, result.Cost)
	require.NotNil(t, result.ThinkingLevelMap[ThinkingHigh])
	require.NotNil(t, result.ThinkingLevelMap[ThinkingLow])
	assert.Equal(t, "enabled", *result.ThinkingLevelMap[ThinkingHigh])
	assert.Equal(t, "low", *result.ThinkingLevelMap[ThinkingLow])

	assert.Equal(t, map[string]string{mergeTestHeaderKey: mergeTestBase}, model.Headers)
	assert.Equal(t, map[string]any{mergeTestModeKey: mergeTestBase}, model.Compat)
}

func TestMergeCustomModelsReplacesMatchingModel(t *testing.T) {
	t.Parallel()

	builtIns := []Model{
		mergeTestModel("same", "Built-in"),
		mergeTestModel("other", "Other"),
	}
	custom := []Model{
		mergeTestModel("same", "Custom"),
		mergeTestModel("new", "New"),
	}

	merged := mergeCustomModels(builtIns, custom)

	require.Len(t, merged, 3)
	assert.Equal(t, "Custom", merged[0].Name)
	assert.Equal(t, "Other", merged[1].Name)
	assert.Equal(t, "New", merged[2].Name)
}

func mergeTestModel(modelID, name string) Model {
	return Model{
		ThinkingLevelMap: nil,
		Headers:          nil,
		Compat:           nil,
		Provider:         mergeTestProvider,
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
