package terminal

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/omarluq/librecode/internal/transcript"

	"github.com/omarluq/librecode/internal/model"
)

func (app *App) showContextInfo(ctx context.Context, original string) error {
	if !app.tokenUsage.HasAny() && !isContextCommand(original) {
		app.sendPrompt(ctx, original)
		return nil
	}

	usage := app.contextUsage(ctx)
	lines := []string{"context:"}
	if summary := formatContextUsage(usage); summary != "" {
		lines = append(lines, "- "+summary)
	}
	breakdownLines := contextBreakdownLines(usage.Breakdown)
	if len(breakdownLines) > 0 {
		lines = append(lines, "- breakdown:")
		lines = append(lines, breakdownLines...)
	}
	contributorLines := contextContributorLines(usage.TopContributors)
	if len(contributorLines) > 0 {
		lines = append(lines, "- top contributors:")
		lines = append(lines, contributorLines...)
	}
	app.addMessage(transcript.RoleCustom, strings.Join(lines, "\n"))

	return nil
}

func isContextCommand(original string) bool {
	trimmed := strings.TrimSpace(original)
	return trimmed == "/context" || strings.HasPrefix(trimmed, "/context ")
}

func contextBreakdownLines(breakdown map[string]int) []string {
	if len(breakdown) == 0 {
		return nil
	}
	keys := make([]string, 0, len(breakdown))
	for key := range breakdown {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		if breakdown[key] <= 0 {
			continue
		}
		lines = append(lines, fmt.Sprintf("  - %s: %s", key, compactCount(breakdown[key])))
	}

	return lines
}

func (app *App) contextUsage(ctx context.Context) model.TokenUsage {
	if app.tokenUsage.HasAny() {
		return app.tokenUsage
	}
	if app.runtime == nil {
		return model.EmptyTokenUsage()
	}
	usage, err := app.runtime.ContextUsage(ctx, app.sessionID, app.cwd)
	if err != nil {
		return model.EmptyTokenUsage()
	}
	return usage
}
