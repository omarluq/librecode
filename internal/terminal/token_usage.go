package terminal

import (
	"fmt"
	"maps"
	"strconv"

	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/units"
)

const compactWholeThousandsThreshold = 10 * units.TokenThousand

func (app *App) applyTokenUsage(usage *model.TokenUsage) {
	app.applyTokenUsageEvent(usage, false)
}

func (app *App) applyTokenUsageEvent(usage *model.TokenUsage, snapshot bool) {
	if usage == nil || !usage.HasAny() {
		return
	}

	if snapshot {
		app.tokenUsage = cloneTerminalUsage(usage)

		return
	}

	app.tokenUsage = mergeTerminalUsage(&app.tokenUsage, usage)
}

func cloneTerminalUsage(usage *model.TokenUsage) model.TokenUsage {
	cloned := *usage
	cloned.Breakdown = cloneTokenBreakdown(usage.Breakdown)
	cloned.TopContributors = model.CloneTokenContributors(usage.TopContributors)

	return cloned
}

func mergeTerminalUsage(current, next *model.TokenUsage) model.TokenUsage {
	merged := *current
	if next.Provenance != "" {
		merged.Provenance = next.Provenance
	}

	if next.ContextWindow > 0 {
		merged.ContextWindow = next.ContextWindow
	}

	if next.ContextTokens > 0 {
		merged.ContextTokens = next.ContextTokens
	}

	if len(next.Breakdown) > 0 {
		merged.Breakdown = cloneTokenBreakdown(next.Breakdown)
	}

	if len(next.TopContributors) > 0 {
		merged.TopContributors = model.CloneTokenContributors(next.TopContributors)
	}

	return merged
}

func (app *App) tokenStatusText() string {
	return formatTokenStatus(&app.tokenUsage)
}

func formatTokenStatus(usage *model.TokenUsage) string {
	if !usage.HasAny() {
		return ""
	}

	contextText := formatContextUsage(usage)
	if contextText == "" {
		return ""
	}

	return contextText
}

func formatContextUsage(usage *model.TokenUsage) string {
	window := usage.ContextWindow
	switch {
	case usage.ContextTokens > 0 && window > 0:
		return fmt.Sprintf(
			"ctx %s/%s %d%%",
			compactCount(usage.ContextTokens),
			compactCount(window),
			units.PercentOf(usage.ContextTokens, window),
		)
	case usage.ContextTokens > 0:
		return "ctx " + compactCount(usage.ContextTokens)
	case window > 0:
		return ""
	default:
		return ""
	}
}

func cloneTokenBreakdown(values map[string]int) map[string]int {
	cloned := make(map[string]int, len(values))
	maps.Copy(cloned, values)

	return cloned
}

func compactCount(value int) string {
	return compactCount64(int64(value))
}

func compactCount64(value int64) string {
	if value >= int64(units.TokenMillion) {
		return fmt.Sprintf("%.1fm", float64(value)/units.TokenMillion)
	}

	if value >= int64(compactWholeThousandsThreshold) {
		return fmt.Sprintf("%dk", value/int64(units.TokenThousand))
	}

	if value >= int64(units.TokenThousand) {
		return fmt.Sprintf("%.1fk", float64(value)/units.TokenThousand)
	}

	return strconv.FormatInt(value, 10)
}
