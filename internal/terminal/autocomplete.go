package terminal

import (
	"fmt"
	"github.com/omarluq/librecode/internal/terminal/rendertext"
	"strings"
	"time"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/transcript"
)

type slashSuggestion struct {
	Name        string
	Description string
}

func slashSuggestions() []slashSuggestion {
	return []slashSuggestion{
		{Name: "auth", Description: "show auth status"},
		{Name: changelogCommandName, Description: "open changelog"},
		{Name: "clone", Description: "clone current session"},
		{Name: "compact", Description: "compact conversation context"},
		{Name: "context", Description: "show context token breakdown"},
		{Name: "copy", Description: "copy the last assistant message"},
		{Name: "export", Description: "export current session"},
		{Name: "fork", Description: "fork current session"},
		{Name: hotkeysCommandName, Description: "show keybindings"},
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

func (app *App) autocompleteLines(width int) []rendertext.Line {
	matches := app.autocompleteMatches()
	if len(matches) == 0 {
		return nil
	}
	selected := app.selectedAutocompleteIndex(matches)
	limit := min(6, len(matches))
	start := autocompleteWindowStart(selected, limit, len(matches))
	lines := make([]rendertext.Line, 0, limit+1)
	lines = append(lines, rendertext.NewLine(
		app.theme.background(colorCustomMessageBg).Bold(true),
		rendertext.PadRight("  slash commands  tab/enter to complete", width),
	))
	for offset := range limit {
		index := start + offset
		match := matches[index]
		prefix := "  "
		style := app.theme.background(colorCustomMessageBg)
		if index == selected {
			prefix = "› "
			style = style.Bold(true)
		}
		text := fmt.Sprintf("%s/%-15s %s", prefix, match.Name, match.Description)
		lines = append(lines, rendertext.NewLine(style, rendertext.PadRight(text, width)))
	}

	return lines
}

func autocompleteWindowStart(selected, limit, total int) int {
	if total <= limit || selected < limit {
		return 0
	}
	start := selected - limit + 1
	if start+limit > total {
		return max(0, total-limit)
	}

	return start
}

func (app *App) autocompleteActive() bool {
	return len(app.autocompleteMatches()) > 0
}

func (app *App) handleAutocompleteKey(event *tcell.EventKey) bool {
	if event.Key() == tcell.KeyEscape {
		app.closeAutocomplete()
		return true
	}
	if app.keys.matches(event, actionCursorDown) {
		return app.moveAutocompleteSelection(1)
	}
	if app.keys.matches(event, actionCursorUp) {
		return app.moveAutocompleteSelection(-1)
	}
	if app.keys.matches(event, actionInputSubmit) || app.keys.matches(event, actionInputTab) {
		return app.acceptAutocomplete()
	}

	return false
}

func (app *App) acceptAutocomplete() bool {
	matches := app.autocompleteMatches()
	if len(matches) == 0 {
		return false
	}
	selected := app.selectedAutocompleteIndex(matches)
	app.resetPromptHistoryNavigation()
	app.setComposerText("/" + matches[selected].Name + " ")
	app.setStatus("completed /" + matches[selected].Name)
	app.resetAutocompleteSelection()

	return true
}

func (app *App) moveAutocompleteSelection(delta int) bool {
	matches := app.autocompleteMatches()
	if len(matches) == 0 {
		app.resetAutocompleteSelection()
		return false
	}
	app.autocompleteSelection = (app.selectedAutocompleteIndex(matches) + delta + len(matches)) % len(matches)

	return true
}

func (app *App) resetAutocompleteSelection() {
	app.autocompleteSelection = 0
	app.autocompleteClosed = false
}

func (app *App) closeAutocomplete() {
	app.autocompleteSelection = 0
	app.autocompleteClosed = true
}

func (app *App) selectedAutocompleteIndex(matches []slashSuggestion) int {
	if len(matches) == 0 || app.autocompleteSelection < 0 || app.autocompleteSelection >= len(matches) {
		return 0
	}

	return app.autocompleteSelection
}

func (app *App) autocompleteMatches() []slashSuggestion {
	text := app.composerText()
	if app.autocompleteClosed {
		return nil
	}
	if strings.Contains(text, "\n") {
		app.resetAutocompleteSelection()
		return nil
	}
	trimmed := strings.TrimLeft(text, " \t")
	if !strings.HasPrefix(trimmed, "/") || strings.Contains(trimmed, " ") {
		app.resetAutocompleteSelection()
		return nil
	}
	query := strings.TrimPrefix(trimmed, "/")
	matches := []slashSuggestion{}
	for _, suggestion := range app.allSlashSuggestions() {
		if strings.HasPrefix(suggestion.Name, query) {
			matches = append(matches, suggestion)
		}
	}

	if app.autocompleteSelection >= len(matches) {
		app.resetAutocompleteSelection()
	}

	return matches
}

func (app *App) allSlashSuggestions() []slashSuggestion {
	suggestions := append([]slashSuggestion{}, slashSuggestions()...)
	for index := range app.resources.Skills {
		skill := &app.resources.Skills[index]
		if !skill.UserInvocable {
			continue
		}
		suggestions = append(suggestions, slashSuggestion{
			Name:        "skill:" + skill.Name,
			Description: skill.Description,
		})
	}

	return suggestions
}

func (app *App) workingIndicator() string {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	frame := frames[app.workFrame%len(frames)]

	return frame + " " + app.workingLoaderText()
}

func (app *App) workingLoaderText() string {
	if app.compacting {
		return "Compacting context..."
	}
	if app.cfg == nil || strings.TrimSpace(app.cfg.App.WorkingLoader.Text) == "" {
		return "Shenaniganing..."
	}

	return app.cfg.App.WorkingLoader.Text
}

func (app *App) workingIndicatorStyle() tcell.Style {
	return tcell.StyleDefault.Foreground(app.workingShimmerBaseColor()).Bold(true)
}

func (app *App) workingShimmerBaseColor() tcell.Color {
	if app.compacting {
		return compactingShimmerBaseColor()
	}

	return defaultWorkingShimmerBaseColor()
}

func defaultWorkingShimmerBaseColor() tcell.Color {
	return hexColor(0x4f6f6a)
}

func defaultWorkingShimmerBrightColor() tcell.Color {
	return hexColor(0xc7fff6)
}

func compactingShimmerBaseColor() tcell.Color {
	return hexColor(0x6f5fa8)
}

func compactingShimmerBrightColor() tcell.Color {
	return hexColor(0xf2d5ff)
}

func (app *App) workingShimmerPosition(contentWidth int) int {
	if contentWidth <= 1 {
		return 0
	}
	if app.workStartedAt.IsZero() {
		return app.workFrame % contentWidth
	}
	elapsed := time.Since(app.workStartedAt) % loaderShimmerSweepDuration
	progress := float64(elapsed) / float64(loaderShimmerSweepDuration)

	return min(contentWidth-1, int(progress*float64(contentWidth)))
}

type workingShimmerPalette struct {
	base   tcell.Color
	bright tcell.Color
	trail1 tcell.Color
	trail2 tcell.Color
	trail3 tcell.Color
}

func defaultWorkingShimmerPalette() workingShimmerPalette {
	return workingShimmerPalette{
		base:   defaultWorkingShimmerBaseColor(),
		bright: defaultWorkingShimmerBrightColor(),
		trail1: hexColor(0xa8f2e9),
		trail2: hexColor(0x86d8d0),
		trail3: hexColor(0x6aaea8),
	}
}

func compactingShimmerPalette() workingShimmerPalette {
	return workingShimmerPalette{
		base:   compactingShimmerBaseColor(),
		bright: compactingShimmerBrightColor(),
		trail1: hexColor(0xe9c6ff),
		trail2: hexColor(0xcfa0e8),
		trail3: hexColor(0xa77ac0),
	}
}

func (app *App) workingShimmerPalette() workingShimmerPalette {
	if app.compacting {
		return compactingShimmerPalette()
	}

	return defaultWorkingShimmerPalette()
}

func workingShimmerColor(position, column, contentWidth int, palette workingShimmerPalette) tcell.Color {
	if contentWidth <= 0 || column < 0 || column >= contentWidth {
		return palette.base
	}
	distanceBehindHead := position - column

	switch distanceBehindHead {
	case 0:
		return palette.bright
	case 1:
		return palette.trail1
	case 2:
		return palette.trail2
	case 3:
		return palette.trail3
	default:
		return palette.base
	}
}

func formatToolEventForUI(event *assistant.ToolEvent) string {
	if event == nil {
		return transcript.FormatToolEventDisplay(nil)
	}
	return transcript.FormatToolEventDisplay(&transcript.ToolEvent{
		Name:          event.Name,
		ArgumentsJSON: event.ArgumentsJSON,
		DetailsJSON:   event.DetailsJSON,
		Result:        event.Result,
		Error:         event.Error,
		IsError:       event.IsError,
	})
}
