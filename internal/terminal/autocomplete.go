package terminal

import (
	"fmt"
	"strings"

	"github.com/omarluq/librecode/internal/assistant"
)

type slashSuggestion struct {
	Name        string
	Description string
}

func slashSuggestions() []slashSuggestion {
	return []slashSuggestion{
		{Name: "auth", Description: "show auth status"},
		{Name: "changelog", Description: "open changelog"},
		{Name: "clone", Description: "clone current session"},
		{Name: "compact", Description: "compact conversation context"},
		{Name: "copy", Description: "copy the last assistant message"},
		{Name: "export", Description: "export current session"},
		{Name: "fork", Description: "fork current session"},
		{Name: "hotkeys", Description: "show keybindings"},
		{Name: "import", Description: "import a session"},
		{Name: commandLogin, Description: "authenticate a provider"},
		{Name: commandLogout, Description: "clear provider auth"},
		{Name: "model", Description: "select provider/model"},
		{Name: "name", Description: "rename current session"},
		{Name: "new", Description: "start a new session"},
		{Name: "quit", Description: "exit librecode"},
		{Name: "reload", Description: "reload auth/model runtime state"},
		{Name: "resume", Description: "open session picker"},
		{Name: "scoped-models", Description: "select scoped model set"},
		{Name: "session", Description: "show current session details"},
		{Name: "settings", Description: "open settings"},
		{Name: "share", Description: "share current session"},
		{Name: "skill", Description: "list or load an Agent Skill"},
		{Name: toolSectionTool, Description: "run a built-in tool with JSON arguments"},
		{Name: "tree", Description: "open session tree"},
	}
}

func (app *App) autocompleteLines(width int) []styledLine {
	matches := app.autocompleteMatches()
	if len(matches) == 0 {
		return nil
	}
	limit := min(6, len(matches))
	lines := make([]styledLine, 0, limit+1)
	lines = append(lines, styledLine{
		Style: app.theme.background(colorCustomMessageBg).Bold(true),
		Text:  padRight("  slash commands  tab to complete", width),
	})
	for index := 0; index < limit; index++ {
		match := matches[index]
		prefix := "  "
		if index == 0 {
			prefix = "› "
		}
		text := fmt.Sprintf("%s/%-15s %s", prefix, match.Name, match.Description)
		lines = append(lines, styledLine{
			Style: app.theme.background(colorCustomMessageBg),
			Text:  padRight(text, width),
		})
	}

	return lines
}

func (app *App) acceptAutocomplete() bool {
	matches := app.autocompleteMatches()
	if len(matches) == 0 {
		return false
	}
	app.resetPromptHistoryNavigation()
	app.setComposerText("/" + matches[0].Name + " ")
	app.setStatus("completed /" + matches[0].Name)

	return true
}

func (app *App) autocompleteMatches() []slashSuggestion {
	text := app.composerText()
	if strings.Contains(text, "\n") {
		return nil
	}
	trimmed := strings.TrimLeft(text, " \t")
	if !strings.HasPrefix(trimmed, "/") || strings.Contains(trimmed, " ") {
		return nil
	}
	query := strings.TrimPrefix(trimmed, "/")
	matches := []slashSuggestion{}
	for _, suggestion := range slashSuggestions() {
		if strings.HasPrefix(suggestion.Name, query) {
			matches = append(matches, suggestion)
		}
	}

	return matches
}

func (app *App) workingIndicator() string {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	frame := frames[app.workFrame%len(frames)]

	return frame + " working…"
}

func formatToolEventForUI(event *assistant.ToolEvent) string {
	parts := []string{fmt.Sprintf("tool: %s", event.Name)}
	if strings.TrimSpace(event.ArgumentsJSON) != "" {
		parts = append(parts, "arguments:", event.ArgumentsJSON)
	}
	if event.Error != "" {
		parts = append(parts, "error:", event.Error)
	}
	if strings.TrimSpace(event.DetailsJSON) != "" {
		parts = append(parts, "details:", event.DetailsJSON)
	}
	if strings.TrimSpace(event.Result) != "" {
		parts = append(parts, "output:", event.Result)
	}

	return strings.Join(parts, "\n")
}
