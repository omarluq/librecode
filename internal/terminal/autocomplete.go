package terminal

import (
	"strings"
	"time"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/transcript"
	"github.com/omarluq/librecode/internal/tui"
)

func slashSuggestions() []tui.ListItem {
	return []tui.ListItem{
		autocompleteSuggestion("auth", "show auth status"),
		autocompleteSuggestion(changelogCommandName, "open changelog"),
		autocompleteSuggestion("clone", "clone current session"),
		autocompleteSuggestion("compact", "compact conversation context"),
		autocompleteSuggestion("context", "show context token breakdown"),
		autocompleteSuggestion("copy", "copy the last assistant message"),
		autocompleteSuggestion("fork", "fork current session"),
		autocompleteSuggestion(hotkeysCommandName, "show keybindings"),
		autocompleteSuggestion(commandLogin, "authenticate a provider"),
		autocompleteSuggestion(commandLogout, "clear provider auth"),
		autocompleteSuggestion("model", "select provider/model"),
		autocompleteSuggestion("name", "rename current session"),
		autocompleteSuggestion("new", "start a new session"),
		autocompleteSuggestion("quit", "exit librecode"),
		autocompleteSuggestion("reload", "reload auth/model runtime state"),
		autocompleteSuggestion("resume", "open session picker"),
		autocompleteSuggestion("scoped-models", "select scoped model set"),
		autocompleteSuggestion("session", "show current session details"),
		autocompleteSuggestion("settings", "open settings"),
		autocompleteSuggestion("skill", "list or load an Agent Skill"),
		autocompleteSuggestion(toolSectionTool, "run a built-in tool with JSON arguments"),
		autocompleteSuggestion("tree", "open session tree"),
	}
}

func autocompleteSuggestion(value, description string) tui.ListItem {
	return tui.ListItem{
		Value:       value,
		Title:       "/" + value,
		Description: description,
		Meta:        "",
	}
}

func (app *App) autocompleteLines(width int) []tui.Line {
	completion := app.autocomplete()
	if completion == nil {
		return nil
	}

	return completion.Render(&tui.AutocompleteRenderOptions{
		Styles: tui.AutocompleteStyles{
			Header:   app.theme.background(colorCustomMessageBg).Bold(true),
			Text:     app.theme.background(colorCustomMessageBg),
			Selected: app.theme.background(colorCustomMessageBg).Bold(true),
		},
		Header:         "  slash commands  tab/enter to complete",
		ItemPrefix:     "  ",
		SelectedPrefix: "› ",
		Width:          width,
		MaxItems:       maxAutocompleteMatches,
		LabelWidth:     slashAutocompleteLabelWidth,
	})
}

func (app *App) autocompleteActive() bool {
	return app.autocomplete() != nil
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
	completion := app.autocomplete()
	if completion == nil {
		return false
	}

	item, ok := completion.SelectedItem()
	if !ok {
		return false
	}

	app.resetPromptHistoryNavigation()
	app.composerBuffer.SetText("/" + item.Value + " ")
	app.setStatus("completed /" + item.Value)
	app.resetAutocompleteSelection()

	return true
}

func (app *App) moveAutocompleteSelection(delta int) bool {
	completion := app.autocomplete()
	if completion == nil {
		app.resetAutocompleteSelection()

		return false
	}

	completion.MoveSelection(delta)
	app.autocompleteSelection = completion.SelectedIndex()

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

func (app *App) autocomplete() *tui.Autocomplete {
	items := app.autocompleteItems()
	if len(items) == 0 {
		return nil
	}

	completion := tui.NewAutocomplete(items)
	completion.SetSelectedIndex(app.autocompleteSelection)
	app.autocompleteSelection = completion.SelectedIndex()

	return completion
}

func (app *App) autocompleteItems() []tui.ListItem {
	query, ok := app.autocompleteQuery()
	if !ok {
		return nil
	}

	items := []tui.ListItem{}

	for _, suggestion := range app.allSlashSuggestions() {
		if strings.HasPrefix(suggestion.Value, query) {
			items = append(items, suggestion)
		}
	}

	return items
}

func (app *App) autocompleteQuery() (string, bool) {
	text := app.composerBuffer.TextValue()
	if app.autocompleteClosed {
		return "", false
	}

	if strings.Contains(text, "\n") {
		app.resetAutocompleteSelection()

		return "", false
	}

	trimmed := strings.TrimLeft(text, " \t")
	if !strings.HasPrefix(trimmed, "/") || strings.Contains(trimmed, " ") {
		app.resetAutocompleteSelection()

		return "", false
	}

	return strings.TrimPrefix(trimmed, "/"), true
}

func (app *App) allSlashSuggestions() []tui.ListItem {
	suggestions := append([]tui.ListItem{}, slashSuggestions()...)

	for index := range app.resources.Skills {
		skill := &app.resources.Skills[index]
		if !skill.UserInvocable {
			continue
		}

		name := "skill:" + skill.Name
		suggestions = append(suggestions, tui.ListItem{
			Value:       name,
			Title:       "/" + name,
			Description: skill.Description,
			Meta:        "",
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
	return hexColorFromString("#4f6f6a")
}

func defaultWorkingShimmerBrightColor() tcell.Color {
	return hexColorFromString("#c7fff6")
}

func compactingShimmerBaseColor() tcell.Color {
	return hexColorFromString("#6f5fa8")
}

func compactingShimmerBrightColor() tcell.Color {
	return hexColorFromString("#f2d5ff")
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
		trail1: hexColorFromString("#a8f2e9"),
		trail2: hexColorFromString("#86d8d0"),
		trail3: hexColorFromString("#6aaea8"),
	}
}

func compactingShimmerPalette() workingShimmerPalette {
	return workingShimmerPalette{
		base:   compactingShimmerBaseColor(),
		bright: compactingShimmerBrightColor(),
		trail1: hexColorFromString("#e9c6ff"),
		trail2: hexColorFromString("#cfa0e8"),
		trail3: hexColorFromString("#a77ac0"),
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
	case compactStageNear:
		return palette.trail2
	case compactStageFar:
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
