package terminal

import (
	"fmt"

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
	switch {
	case usage.ContextTokens > 0 && usage.ContextWindow > 0:
		return fmt.Sprintf(
			"ctx %s/%s %d%%",
			compactCount(usage.ContextTokens),
			compactCount(usage.ContextWindow),
			usage.ContextPercent(),
		)
	case usage.ContextTokens > 0:
		return "ctx " + compactCount(usage.ContextTokens)
	case usage.ContextWindow > 0:
		return "ctx 0/" + compactCount(usage.ContextWindow)
	default:
		return ""
	}
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
