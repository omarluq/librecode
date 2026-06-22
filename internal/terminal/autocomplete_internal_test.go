package terminal

import (
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/core"
	"github.com/omarluq/librecode/internal/tui"
)

func TestSlashSuggestionsOnlyIncludesImplementedCommands(t *testing.T) {
	t.Parallel()

	names := make([]string, 0, len(slashSuggestions()))
	for _, suggestion := range slashSuggestions() {
		names = append(names, suggestion.Value)
	}

	assert.Contains(t, names, "skill")
	assert.NotContains(t, names, "export")
	assert.NotContains(t, names, "import")
	assert.NotContains(t, names, "share")
}

func TestAutocompleteMatchesIncludesUserInvocableSkills(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.resources.Skills = []core.Skill{
		testAutocompleteSkill("fix-bug", "Fix bugs safely", true),
		testAutocompleteSkill("hidden", "Hidden skill", false),
	}
	app.composerBuffer.SetText("/skf")

	matches := app.autocompleteItems()

	requireSuggestion(t, matches, "skill:fix-bug")
	assert.NotContains(t, suggestionNames(matches), "skill:hidden")
}

func TestSlashAutocompleteMatchesKeepsPrefixMatchesFirst(t *testing.T) {
	t.Parallel()

	suggestions := []tui.ListItem{
		autocompleteSuggestion("resume", "open session picker"),
		autocompleteSuggestion("scoped-models", "select scoped model set"),
		autocompleteSuggestion("session", "show current session details"),
		autocompleteSuggestion("settings", "open settings"),
		autocompleteSuggestion("skill", "list or load an Agent Skill"),
	}

	matches := slashAutocompleteMatches("se", suggestions)

	assert.Equal(t, []string{"session", "settings", "scoped-models", "resume"}, suggestionNames(matches))
}

func TestAutocompleteRendersWithReusableComponent(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.composerBuffer.SetText("/s")

	lines := app.autocompleteLines(48)

	assert.NotEmpty(t, lines)
	assert.Contains(t, lines[0].Text, "slash commands")
	assert.Contains(t, lines[1].Text, "› /scoped-models")
	assert.Contains(t, lines[1].Text, "select scoped model set")
}

func TestAutocompleteArrowSelectionAcceptsSelectedSuggestion(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.composerBuffer.SetText("/s")

	pressTerminalKey(t, app, tcell.KeyDown, "")
	pressTerminalKey(t, app, tcell.KeyEnter, "")

	assertEditorText(t, app, "/session ")
}

func TestAutocompleteUpWrapsToLastSuggestion(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.composerBuffer.SetText("/s")

	pressTerminalKey(t, app, tcell.KeyUp, "")
	pressTerminalKey(t, app, tcell.KeyTab, "")

	assertEditorText(t, app, "/skill ")
}

func TestAutocompleteArrowKeysDoNotNavigatePromptHistory(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.recordPromptHistory("previous prompt")
	app.composerBuffer.SetText("/s")

	pressTerminalKey(t, app, tcell.KeyUp, "")

	assertEditorText(t, app, "/s")
}

func TestAutocompleteEscapeClosesSuggestions(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.composerBuffer.SetText("/s")

	pressTerminalKey(t, app, tcell.KeyEscape, "")

	assert.False(t, app.autocompleteActive())
	assertEditorText(t, app, "/s")
}

func TestAutocompleteReopensAfterEditing(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.composerBuffer.SetText("/s")
	pressTerminalKey(t, app, tcell.KeyEscape, "")

	pressTerminalKey(t, app, tcell.KeyRune, "e")

	assert.True(t, app.autocompleteActive())
}

func testAutocompleteSkill(name, description string, userInvocable bool) core.Skill {
	return core.Skill{
		Metadata: nil,
		SourceInfo: core.SourceInfo{
			Path:    "",
			Source:  "",
			Scope:   "",
			Origin:  "",
			BaseDir: "",
		},
		Name:                   name,
		Description:            description,
		FilePath:               "",
		BaseDir:                "",
		License:                "",
		Compatibility:          "",
		AllowedTools:           nil,
		UserInvocable:          userInvocable,
		DisableModelInvocation: false,
	}
}

func requireSuggestion(t *testing.T, suggestions []tui.ListItem, name string) {
	t.Helper()

	assert.Contains(t, suggestionNames(suggestions), name)
}

func suggestionNames(suggestions []tui.ListItem) []string {
	names := make([]string, 0, len(suggestions))
	for _, suggestion := range suggestions {
		names = append(names, suggestion.Value)
	}

	return names
}
