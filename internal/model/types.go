// Package model resolves providers, model metadata, auth, and thinking levels.
package model

import "maps"

// ThinkingLevel controls model reasoning depth.
type ThinkingLevel string

const (
	// ThinkingOff disables reasoning where supported.
	ThinkingOff ThinkingLevel = "off"
	// ThinkingMinimal selects minimal reasoning.
	ThinkingMinimal ThinkingLevel = "minimal"
	// ThinkingLow selects low reasoning.
	ThinkingLow ThinkingLevel = "low"
	// ThinkingMedium selects medium reasoning.
	ThinkingMedium ThinkingLevel = "medium"
	// ThinkingHigh selects high reasoning.
	ThinkingHigh ThinkingLevel = "high"
	// ThinkingXHigh selects extra-high reasoning.
	ThinkingXHigh ThinkingLevel = "xhigh"
)

// InputMode describes a model input modality.
type InputMode string

const (
	// InputText indicates text input support.
	InputText InputMode = "text"
	// InputImage indicates image input support.
	InputImage InputMode = "image"
)

// Cost describes per-token pricing metadata.
type Cost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
}

// Model describes a provider model.
type Model struct {
	ThinkingLevelMap map[ThinkingLevel]*string `json:"thinkingLevelMap,omitempty"`
	Headers          map[string]string         `json:"headers,omitempty"`
	Compat           map[string]any            `json:"compat,omitempty"`
	Provider         string                    `json:"provider"`
	ID               string                    `json:"id"`
	Name             string                    `json:"name"`
	API              string                    `json:"api,omitempty"`
	BaseURL          string                    `json:"baseUrl,omitempty"`
	Input            []InputMode               `json:"input,omitempty"`
	Cost             Cost                      `json:"cost"`
	ContextWindow    int                       `json:"contextWindow,omitempty"`
	MaxTokens        int                       `json:"maxTokens,omitempty"`
	Reasoning        bool                      `json:"reasoning"`
}

// RequestAuth contains resolved per-provider request auth and headers.
type RequestAuth struct {
	Headers map[string]string `json:"headers,omitempty"`
	APIKey  string            `json:"api_key,omitempty"`
	Error   string            `json:"error,omitempty"`
	OK      bool              `json:"ok"`
}

// IsValidThinkingLevel reports whether value is a known thinking level.
func IsValidThinkingLevel(value string) bool {
	switch ThinkingLevel(value) {
	case ThinkingOff, ThinkingMinimal, ThinkingLow, ThinkingMedium, ThinkingHigh, ThinkingXHigh:
		return true
	default:
		return false
	}
}

// ModelsAreEqual compares provider and model ID.
func ModelsAreEqual(left, right *Model) bool {
	return left.Provider == right.Provider && left.ID == right.ID
}

func zeroModel() Model {
	return Model{
		ThinkingLevelMap: nil,
		Headers:          nil,
		Compat:           nil,
		Provider:         "",
		ID:               "",
		Name:             "",
		API:              "",
		BaseURL:          "",
		Input:            nil,
		Cost:             Cost{Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0},
		ContextWindow:    0,
		MaxTokens:        0,
		Reasoning:        false,
	}
}

func cloneModel(model *Model) Model {
	cloned := *model
	cloned.ThinkingLevelMap = cloneThinkingMap(model.ThinkingLevelMap)
	cloned.Headers = cloneStringMap(model.Headers)
	cloned.Compat = cloneAnyMap(model.Compat)
	cloned.Input = append([]InputMode{}, model.Input...)

	return cloned
}

func cloneModels(models []Model) []Model {
	cloned := make([]Model, 0, len(models))
	for index := range models {
		cloned = append(cloned, cloneModel(&models[index]))
	}

	return cloned
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	output := make(map[string]string, len(input))
	maps.Copy(output, input)

	return output
}

func cloneAnyMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	output := make(map[string]any, len(input))
	maps.Copy(output, input)

	return output
}

func cloneThinkingMap(input map[ThinkingLevel]*string) map[ThinkingLevel]*string {
	if input == nil {
		return nil
	}
	output := make(map[ThinkingLevel]*string, len(input))
	for key, value := range input {
		if value == nil {
			output[key] = nil
			continue
		}
		copied := *value
		output[key] = &copied
	}

	return output
}
