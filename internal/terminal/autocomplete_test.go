//nolint:testpackage // These tests exercise unexported terminal autocomplete helpers.
package terminal

import (
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/core"
)

func TestSlashSuggestionsIncludesSkill(t *testing.T) {
	t.Parallel()

	names := make([]string, 0, len(slashSuggestions()))
	for _, suggestion := range slashSuggestions() {
		names = append(names, suggestion.Name)
	}

	assert.Contains(t, names, "skill")
}

func TestAutocompleteMatchesIncludesUserInvocableSkills(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.resources.Skills = []core.Skill{
		testAutocompleteSkill("fix-bug", "Fix bugs safely", true),
		testAutocompleteSkill("hidden", "Hidden skill", false),
	}
	app.setComposerText("/skill:f")

	matches := app.autocompleteMatches()

	requireSuggestion(t, matches, "skill:fix-bug")
	assert.NotContains(t, suggestionNames(matches), "skill:hidden")
}

func TestAutocompleteArrowSelectionAcceptsSelectedSuggestion(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.setComposerText("/s")

	pressTerminalKey(t, app, tcell.KeyDown, "")
	pressTerminalKey(t, app, tcell.KeyEnter, "")

	assertEditorText(t, app, "/session ")
}

func TestAutocompleteUpWrapsToLastSuggestion(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.setComposerText("/s")

	pressTerminalKey(t, app, tcell.KeyUp, "")
	pressTerminalKey(t, app, tcell.KeyTab, "")

	assertEditorText(t, app, "/skill ")
}

func TestAutocompleteArrowKeysDoNotNavigatePromptHistory(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.recordPromptHistory("previous prompt")
	app.setComposerText("/s")

	pressTerminalKey(t, app, tcell.KeyUp, "")

	assertEditorText(t, app, "/s")
}

func TestAutocompleteEscapeClosesSuggestions(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.setComposerText("/s")

	pressTerminalKey(t, app, tcell.KeyEscape, "")

	assert.False(t, app.autocompleteActive())
	assertEditorText(t, app, "/s")
}

func TestAutocompleteReopensAfterEditing(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.setComposerText("/s")
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

func requireSuggestion(t *testing.T, suggestions []slashSuggestion, name string) {
	t.Helper()

	assert.Contains(t, suggestionNames(suggestions), name)
}

func suggestionNames(suggestions []slashSuggestion) []string {
	names := make([]string, 0, len(suggestions))
	for _, suggestion := range suggestions {
		names = append(names, suggestion.Name)
	}

	return names
}
