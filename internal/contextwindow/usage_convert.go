package contextwindow

import (
	"github.com/samber/lo"

	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/mapsutil"
	"github.com/omarluq/librecode/internal/model"
)

func llmUsageFromModel(usage model.TokenUsage) llm.Usage {
	return llm.Usage{
		Breakdown:       mapsutil.CloneOrNil(usage.Breakdown),
		TopContributors: llmTokenContributorsFromModel(usage.TopContributors),
		ContextWindow:   usage.ContextWindow,
		ContextTokens:   usage.ContextTokens,
		InputTokens:     usage.InputTokens,
		OutputTokens:    usage.OutputTokens,
	}
}

func modelUsageFromLLM(usage llm.Usage) model.TokenUsage {
	return model.TokenUsage{
		Breakdown:       mapsutil.CloneOrNil(usage.Breakdown),
		TopContributors: llmTokenContributorsToModel(usage.TopContributors),
		ContextWindow:   usage.ContextWindow,
		ContextTokens:   usage.ContextTokens,
		InputTokens:     usage.InputTokens,
		OutputTokens:    usage.OutputTokens,
	}
}

func llmTokenContributorsFromModel(contributors []model.TokenContributor) []llm.TokenContributor {
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

func llmTokenContributorsToModel(contributors []llm.TokenContributor) []model.TokenContributor {
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
