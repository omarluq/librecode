package llm

import (
	"maps"

	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/tool"
)

// UsageFromModel converts model.TokenUsage into provider-neutral usage.
func UsageFromModel(usage model.TokenUsage) Usage {
	return Usage{
		Breakdown:       cloneIntMap(usage.Breakdown),
		TopContributors: tokenContributorsFromModel(usage.TopContributors),
		ContextWindow:   usage.ContextWindow,
		ContextTokens:   usage.ContextTokens,
		InputTokens:     usage.InputTokens,
		OutputTokens:    usage.OutputTokens,
	}
}

// UsageToModel converts provider-neutral usage into model.TokenUsage.
func UsageToModel(usage Usage) model.TokenUsage {
	return model.TokenUsage{
		Breakdown:       cloneIntMap(usage.Breakdown),
		TopContributors: tokenContributorsToModel(usage.TopContributors),
		ContextWindow:   usage.ContextWindow,
		ContextTokens:   usage.ContextTokens,
		InputTokens:     usage.InputTokens,
		OutputTokens:    usage.OutputTokens,
	}
}

// ModelRefFromModel converts model metadata into a provider-neutral model reference.
func ModelRefFromModel(input *model.Model) ModelRef {
	if input == nil {
		return ModelRef{
			Metadata:      nil,
			Provider:      "",
			ID:            "",
			API:           "",
			BaseURL:       "",
			MaxTokens:     0,
			ContextWindow: 0,
			Reasoning:     false,
		}
	}

	return ModelRef{
		Metadata:      cloneAnyMap(input.Compat),
		Provider:      input.Provider,
		ID:            input.ID,
		API:           input.API,
		BaseURL:       input.BaseURL,
		MaxTokens:     input.MaxTokens,
		ContextWindow: input.ContextWindow,
		Reasoning:     input.Reasoning,
	}
}

// AuthFromModel converts resolved request auth into provider-neutral auth.
func AuthFromModel(auth model.RequestAuth) Auth {
	return Auth{
		Headers: cloneStringMap(auth.Headers),
		APIKey:  auth.APIKey,
	}
}

// ToolDefinitionFromTool converts a local tool definition into provider-neutral metadata.
func ToolDefinitionFromTool(definition *tool.Definition) ToolDefinition {
	if definition == nil {
		return ToolDefinition{
			Schema:      nil,
			Name:        "",
			Description: "",
			ReadOnly:    false,
		}
	}

	return ToolDefinition{
		Schema:      cloneAnyMap(definition.Schema),
		Name:        string(definition.Name),
		Description: definition.Description,
		ReadOnly:    definition.ReadOnly,
	}
}

func tokenContributorsFromModel(contributors []model.TokenContributor) []TokenContributor {
	if len(contributors) == 0 {
		return nil
	}
	output := make([]TokenContributor, 0, len(contributors))
	for _, contributor := range contributors {
		output = append(output, TokenContributor{
			Label:   contributor.Label,
			Role:    contributor.Role,
			Preview: contributor.Preview,
			Tokens:  contributor.Tokens,
			Chars:   contributor.Chars,
		})
	}

	return output
}

func tokenContributorsToModel(contributors []TokenContributor) []model.TokenContributor {
	if len(contributors) == 0 {
		return nil
	}
	output := make([]model.TokenContributor, 0, len(contributors))
	for _, contributor := range contributors {
		output = append(output, model.TokenContributor{
			Label:   contributor.Label,
			Role:    contributor.Role,
			Preview: contributor.Preview,
			Tokens:  contributor.Tokens,
			Chars:   contributor.Chars,
		})
	}

	return output
}

func cloneIntMap(input map[string]int) map[string]int {
	if input == nil {
		return nil
	}
	output := make(map[string]int, len(input))
	maps.Copy(output, input)

	return output
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
