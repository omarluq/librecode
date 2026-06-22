package terminal

import (
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

const (
	testSlashResume       = "resume"
	testSlashScopedModels = "scoped-models"
	testSlashSession      = "session"
	testSlashSettings     = "settings"
	testSlashSkill        = "skill"
)

func TestAutocompleteMatchesSlashQueries(t *testing.T) {
	t.Parallel()

	testSkills := []core.Skill{
		testAutocompleteSkill("fix-bug", "Fix bugs safely", true),
		testAutocompleteSkill("hidden", "Hidden skill", false),
	}

	tests := []struct {
		name       string
		input      string
		skills     []core.Skill
		want       []string
		wantAbsent []string
	}{
		{
			name:       "empty query returns slash commands",
			input:      "/",
			skills:     nil,
			want:       []string{"auth", testSlashSkill},
			wantAbsent: nil,
		},
		{
			name:       "single rune query stays prefix only",
			input:      "/s",
			skills:     nil,
			want:       []string{testSlashScopedModels, testSlashSession, testSlashSettings, testSlashSkill},
			wantAbsent: []string{testSlashResume},
		},
		{
			name:       "multi rune query includes fuzzy skill matches",
			input:      "/skf",
			skills:     testSkills,
			want:       []string{"skill:fix-bug"},
			wantAbsent: []string{"skill:hidden"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			app := newRenderTestApp(t)
			app.resources.Skills = testCase.skills
			app.composerBuffer.SetText(testCase.input)

			matches := app.autocompleteItems()

			for _, want := range testCase.want {
				requireSuggestion(t, matches, want)
			}

			names := suggestionNames(matches)
			for _, absent := range testCase.wantAbsent {
				assert.NotContains(t, names, absent)
			}
		})
	}
}

func TestSlashAutocompleteMatchesKeepsPrefixMatchesFirst(t *testing.T) {
	t.Parallel()

	suggestions := []tui.ListItem{
		autocompleteSuggestion(testSlashResume, "open session picker"),
		autocompleteSuggestion(testSlashScopedModels, "select scoped model set"),
		autocompleteSuggestion(testSlashSession, "show current session details"),
		autocompleteSuggestion(testSlashSettings, "open settings"),
		autocompleteSuggestion(testSlashSkill, "list or load an Agent Skill"),
	}

	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{
			name:  "empty query keeps source order",
			query: "",
			want: []string{
				testSlashResume,
				testSlashScopedModels,
				testSlashSession,
				testSlashSettings,
				testSlashSkill,
			},
		},
		{
			name:  "single rune query returns prefix matches only",
			query: "s",
			want:  []string{testSlashScopedModels, testSlashSession, testSlashSettings, testSlashSkill},
		},
		{
			name:  "multi rune query keeps prefix matches before fuzzy matches",
			query: "se",
			want:  []string{testSlashSession, testSlashSettings, testSlashScopedModels, testSlashResume},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			matches := slashAutocompleteMatches(testCase.query, suggestions)

			assert.Equal(t, testCase.want, suggestionNames(matches))
		})
	}
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

	require.Contains(t, suggestionNames(suggestions), name)
}

func suggestionNames(suggestions []tui.ListItem) []string {
	names := make([]string, 0, len(suggestions))
	for _, suggestion := range suggestions {
		names = append(names, suggestion.Value)
	}

	return names
}
