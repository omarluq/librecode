// Package llmconv contains adapters between provider-neutral LLM DTOs and app model DTOs.
package llmconv

import (
	"github.com/samber/lo"

	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/mapsutil"
	"github.com/omarluq/librecode/internal/model"
)

// UsageFromModel converts model token usage into provider-neutral LLM usage.
func UsageFromModel(usage *model.TokenUsage) llm.Usage {
	converted := llm.Usage{
		Breakdown:       mapsutil.CloneOrNil(usage.Breakdown),
		TopContributors: TokenContributorsFromModel(usage.TopContributors),
		ContextWindow:   usage.ContextWindow,
		ContextTokens:   usage.ContextTokens,
		InputTokens:     usage.InputTokens,
		OutputTokens:    usage.OutputTokens,
		Provenance:      llm.UsageProvenance(usage.Provenance),
	}
	if usage.Reported() {
		converted = converted.WithReported()
	}

	return converted
}

// UsageToModel converts provider-neutral LLM usage into model token usage.
func UsageToModel(usage *llm.Usage) model.TokenUsage {
	converted := model.TokenUsage{
		Breakdown:       mapsutil.CloneOrNil(usage.Breakdown),
		TopContributors: TokenContributorsToModel(usage.TopContributors),
		ContextWindow:   usage.ContextWindow,
		ContextTokens:   usage.ContextTokens,
		InputTokens:     usage.InputTokens,
		OutputTokens:    usage.OutputTokens,
		Provenance:      model.UsageProvenance(usage.Provenance),
	}
	if usage.Reported() {
		converted = converted.WithReported()
	}

	return converted
}

// TokenContributorsFromModel converts model token contributors to LLM token contributors.
func TokenContributorsFromModel(contributors []model.TokenContributor) []llm.TokenContributor {
	if len(contributors) == 0 {
		return nil
	}

	return lo.Map(contributors, func(contributor model.TokenContributor, _ int) llm.TokenContributor {
		return llm.TokenContributor{
			Label:   contributor.Label,
			Role:    contributor.Role,
			Preview: contributor.Preview,
			Tokens:  contributor.Tokens,
			Chars:   contributor.Chars,
		}
	})
}

// TokenContributorsToModel converts LLM token contributors to model token contributors.
func TokenContributorsToModel(contributors []llm.TokenContributor) []model.TokenContributor {
	if len(contributors) == 0 {
		return nil
	}

	return lo.Map(contributors, func(contributor llm.TokenContributor, _ int) model.TokenContributor {
		return model.TokenContributor{
			Label:   contributor.Label,
			Role:    contributor.Role,
			Preview: contributor.Preview,
			Tokens:  contributor.Tokens,
			Chars:   contributor.Chars,
		}
	})
}
