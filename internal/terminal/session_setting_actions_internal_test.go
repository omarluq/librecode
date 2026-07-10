package terminal

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/auth"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/testutil"
)

func TestToggleFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		run           func(*App)
		flag          func(*App) bool
		wantOnStatus  string
		wantOffStatus string
	}{
		{
			name:          "tools expanded",
			run:           (*App).toggleToolsExpanded,
			flag:          func(app *App) bool { return app.toolsExpanded },
			wantOnStatus:  "tool output expanded: on",
			wantOffStatus: "tool output expanded: off",
		},
		{
			name:          "thinking hidden",
			run:           (*App).toggleThinkingHidden,
			flag:          func(app *App) bool { return app.hideThinking },
			wantOnStatus:  "thinking hidden: on",
			wantOffStatus: "thinking hidden: off",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			app := newRenderTestApp(t)

			testCase.run(app)

			assert.True(t, testCase.flag(app))
			assert.Equal(t, testCase.wantOnStatus, app.statusMessage)

			testCase.run(app)

			assert.False(t, testCase.flag(app))
			assert.Equal(t, testCase.wantOffStatus, app.statusMessage)
		})
	}
}

func TestCycleThinking(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		current string
		want    model.ThinkingLevel
	}{
		{name: "empty uses next after off", current: "", want: model.ThinkingMinimal},
		{name: "xhigh advances to max", current: string(model.ThinkingXHigh), want: model.ThinkingMax},
		{name: "max wraps to off", current: string(model.ThinkingMax), want: model.ThinkingOff},
		{name: "unknown resets to off", current: "mystery", want: model.ThinkingOff},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			app := newRenderTestApp(t)
			app.cfg = renderParityConfig()
			app.cfg.Assistant.ThinkingLevel = testCase.current

			app.cycleThinking()

			assert.Equal(t, string(testCase.want), app.currentThinkingLevel())
		})
	}
}

const (
	testGPT5ModelID       = "gpt-5"
	testOpenAIGPT5        = testProviderOpenAI + "/" + testGPT5ModelID
	testAnthropicClaudeID = "anthropic/claude"
)

func TestSessionSettingMutatorsPersist(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := newPromptSendTestApp(t, newTerminalPromptClient(newTerminalCompletionResult("ok"), nil))

	session, err := app.runtime.SessionRepository().CreateSession(ctx, app.cwd, "persist", "")
	require.NoError(t, err)

	app.sessionID = session.ID
	app.cfg = renderParityConfig()

	app.setToolsExpanded(true)
	app.setHideThinking(true)
	app.setTheme(lightTheme())
	app.setModelSelection(testProviderOpenAI, testGPT5ModelID)
	app.setThinkingLevelValue(string(model.ThinkingHigh))
	app.setScopedModelEnabled(testOpenAIGPT5, true)
	app.setScopedModelsEnabled([]string{testAnthropicClaudeID}, true)
	app.scopedOrder = []string{testOpenAIGPT5, testAnthropicClaudeID}
	app.setScopedProviderEnabled(testProviderOpenAI, false)

	require.True(t, app.moveScopedModel(testAnthropicClaudeID, -1))

	app.clearScopedModels([]string{testAnthropicClaudeID})

	settings := persistedSessionSettings(ctx, t, app, session.ID)
	assertPersistedSessionSettings(t, &settings)
}

func persistedSessionSettings(
	ctx context.Context,
	t *testing.T,
	app *App,
	sessionID string,
) sessionSettingsDocument {
	t.Helper()

	document, found, err := app.settings.Get(ctx, sessionSettingsNamespace, sessionID)
	require.NoError(t, err)
	require.True(t, found)

	var settings sessionSettingsDocument
	require.NoError(t, json.Unmarshal([]byte(document.ValueJSON), &settings))

	return settings
}

func assertPersistedSessionSettings(t *testing.T, settings *sessionSettingsDocument) {
	t.Helper()

	assert.True(t, settings.ToolsExpanded)
	assert.True(t, settings.HideThinking)
	assert.Equal(t, themeNameLight, settings.Theme)
	assert.Equal(t, testProviderOpenAI, settings.Provider)
	assert.Equal(t, testGPT5ModelID, settings.Model)
	assert.Equal(t, string(model.ThinkingHigh), settings.ThinkingLevel)
	assert.Empty(t, settings.ScopedEnabled)
}

func TestMoveScopedModelGuards(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.scopedOrder = []string{"a", "b"}

	assert.False(t, app.moveScopedModel("missing", 1))
	assert.False(t, app.moveScopedModel("a", -1))
}

func TestHandleScopedModelKeyEnableAll(t *testing.T) {
	t.Parallel()

	app := newScopedModelTestApp(t)
	require.True(t, app.handleScopedModelKey(tcell.NewEventKey(tcell.KeyCtrlA, "", tcell.ModNone)))

	for _, item := range app.panel.FilteredItems() {
		assert.True(t, app.scopedEnabled[item.Value], "expected scoped model %q to be enabled", item.Value)
	}
}

func TestHandleScopedModelKeyClearAll(t *testing.T) {
	t.Parallel()

	app := newScopedModelTestApp(t)
	for _, item := range app.panel.FilteredItems() {
		app.scopedEnabled[item.Value] = true
	}

	require.True(t, app.handleScopedModelKey(tcell.NewEventKey(tcell.KeyCtrlX, "", tcell.ModNone)))

	for _, item := range app.panel.FilteredItems() {
		assert.False(t, app.scopedEnabled[item.Value], "expected scoped model %q to be cleared", item.Value)
	}
}

func TestHandleScopedModelKeyToggleProvider(t *testing.T) {
	t.Parallel()

	app := newScopedModelTestApp(t)
	require.True(t, app.handleScopedModelKey(tcell.NewEventKey(tcell.KeyCtrlP, "", tcell.ModNone)))

	items := app.panel.FilteredItems()

	provider := providerFromModelValue(items[0].Value)
	for _, item := range items {
		if providerFromModelValue(item.Value) == provider {
			assert.True(t, app.scopedEnabled[item.Value], "expected provider-scoped model %q to be enabled", item.Value)
		}
	}
}

func TestHandleScopedModelKeyReorderUp(t *testing.T) {
	t.Parallel()

	app := newScopedModelTestApp(t)
	items := app.panel.FilteredItems()

	app.scopedOrder = make([]string, 0, len(items))
	for _, item := range items {
		app.scopedOrder = append(app.scopedOrder, item.Value)
	}

	app.panel.SetSelectedIndex(1)

	selected := items[1].Value

	require.True(t, app.handleScopedModelKey(tcell.NewEventKey(tcell.KeyUp, "", tcell.ModAlt)))
	assert.Equal(t, selected, app.scopedOrder[0])
}

func TestHandleScopedModelKeySave(t *testing.T) {
	t.Parallel()

	app := newScopedModelTestApp(t)
	require.True(t, app.handleScopedModelKey(tcell.NewEventKey(tcell.KeyCtrlS, "", tcell.ModNone)))

	assert.Empty(t, app.selectedPanelKind)
	assert.Nil(t, app.panel)
}

func newScopedModelTestApp(t *testing.T) *App {
	t.Helper()

	app := newRenderTestApp(t)

	storage := testutil.NewAuthStorage(t, map[string]auth.Credential{
		promptSendTestProvider: testPanelAuthCredential(),
	})

	app.models = model.NewRegistry(&model.RegistryOptions{
		ConfigReader: nil,
		Auth:         storage,
		ModelsPath:   "",
		BuiltIns: []model.Model{
			newPanelTestModel("test-model-a", "Test Model A"),
			newPanelTestModel("test-model-b", "Test Model B"),
		},
		Discovery: disabledModelDiscovery(),
	})
	app.scopedEnabled = map[string]bool{}
	app.openScopedModelsPanel()

	require.GreaterOrEqual(t, len(app.panel.FilteredItems()), 2)

	return app
}
