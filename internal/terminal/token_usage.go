package terminal

import (
	"fmt"
	"maps"

	"github.com/omarluq/librecode/internal/model"
)

func (app *App) applyTokenUsage(usage *model.TokenUsage) {
	if usage == nil || !usage.HasAny() {
		return
	}
	app.tokenUsage = mergeTerminalUsage(app.tokenUsage, *usage)
}

func mergeTerminalUsage(current, next model.TokenUsage) model.TokenUsage {
	if next.ContextWindow > 0 {
		current.ContextWindow = next.ContextWindow
	}
	if next.ContextTokens > current.ContextTokens {
		current.ContextTokens = next.ContextTokens
	}
	if len(next.Breakdown) > 0 {
		current.Breakdown = cloneTokenBreakdown(next.Breakdown)
	}
	if len(next.TopContributors) > 0 {
		current.TopContributors = cloneTokenContributors(next.TopContributors)
	}

	return current
}

func (app *App) tokenStatusText() string {
	return formatTokenStatus(app.tokenUsage)
}

func formatTokenStatus(usage model.TokenUsage) string {
	if !usage.HasAny() {
		return ""
	}
	contextText := formatContextUsage(usage)
	if contextText == "" {
		return ""
	}

	return contextText
}

func formatContextUsage(usage model.TokenUsage) string {
	budget := contextDisplayBudget(usage)
	switch {
	case usage.ContextTokens > 0 && budget > 0:
		return fmt.Sprintf(
			"ctx %s/%s %d%%",
			compactCount(usage.ContextTokens),
			compactCount(budget),
			percentOf(usage.ContextTokens, budget),
		)
	case usage.ContextTokens > 0:
		return "ctx " + compactCount(usage.ContextTokens)
	case budget > 0:
		return "ctx 0/" + compactCount(budget)
	default:
		return ""
	}
}

func contextDisplayBudget(usage model.TokenUsage) int {
	if usableInput := usableInputBudget(usage); usableInput > 0 {
		return usableInput
	}

	return usage.ContextWindow
}

func usableInputBudget(usage model.TokenUsage) int {
	if len(usage.Breakdown) == 0 {
		return 0
	}

	return usage.Breakdown["usable_input"]
}

func percentOf(tokens, budget int) int {
	if tokens <= 0 || budget <= 0 {
		return 0
	}

	return tokens * 100 / budget
}

func cloneTokenBreakdown(values map[string]int) map[string]int {
	cloned := make(map[string]int, len(values))
	maps.Copy(cloned, values)

	return cloned
}

func cloneTokenContributors(contributors []model.TokenContributor) []model.TokenContributor {
	if len(contributors) == 0 {
		return nil
	}
	cloned := make([]model.TokenContributor, len(contributors))
	copy(cloned, contributors)

	return cloned
}

func compactCount(value int) string {
	if value >= 1_000_000 {
		return fmt.Sprintf("%.1fm", float64(value)/1_000_000)
	}
	if value >= 10_000 {
		return fmt.Sprintf("%dk", value/1_000)
	}
	if value >= 1_000 {
		return fmt.Sprintf("%.1fk", float64(value)/1_000)
	}

	return fmt.Sprintf("%d", value)
}
