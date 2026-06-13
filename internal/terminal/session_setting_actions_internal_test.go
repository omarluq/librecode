package terminal

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/omarluq/librecode/internal/auth"
	"github.com/omarluq/librecode/internal/model"
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

			if !testCase.flag(app) {
				t.Fatal("flag should be true after first toggle")
			}

			if got := app.statusMessage; got != testCase.wantOnStatus {
				t.Fatalf("statusMessage = %q, want %q", got, testCase.wantOnStatus)
			}

			testCase.run(app)

			if testCase.flag(app) {
				t.Fatal("flag should be false after second toggle")
			}

			if got := app.statusMessage; got != testCase.wantOffStatus {
				t.Fatalf("statusMessage = %q, want %q", got, testCase.wantOffStatus)
			}
		})
	}
}

func TestCycleThinking(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.cfg = renderParityConfig()
	app.cfg.Assistant.ThinkingLevel = ""

	app.cycleThinking()

	if got, want := app.currentThinkingLevel(), string(model.ThinkingMinimal); got != want {
		t.Fatalf("thinking level = %q, want %q", got, want)
	}

	app.setThinkingLevel(string(model.ThinkingXHigh))
	app.cycleThinking()

	if got, want := app.currentThinkingLevel(), string(model.ThinkingOff); got != want {
		t.Fatalf("thinking level = %q, want %q", got, want)
	}

	app.setThinkingLevel("mystery")
	app.cycleThinking()

	if got, want := app.currentThinkingLevel(), string(model.ThinkingOff); got != want {
		t.Fatalf("thinking level after fallback = %q, want %q", got, want)
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
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

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

	if !app.moveScopedModel(testAnthropicClaudeID, -1) {
		t.Fatal("moveScopedModel should succeed")
	}

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
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}

	if !found {
		t.Fatal("expected persisted session settings")
	}

	var settings sessionSettingsDocument
	if err := json.Unmarshal([]byte(document.ValueJSON), &settings); err != nil {
		t.Fatalf("unmarshal settings: %v", err)
	}

	return settings
}

func assertPersistedSessionSettings(t *testing.T, settings *sessionSettingsDocument) {
	t.Helper()

	if !settings.ToolsExpanded || !settings.HideThinking {
		t.Fatalf("persisted booleans = %+v", settings)
	}

	if settings.Theme != themeNameLight {
		t.Fatalf("theme = %q, want %s", settings.Theme, themeNameLight)
	}

	if settings.Provider != testProviderOpenAI || settings.Model != testGPT5ModelID {
		t.Fatalf(
			"model selection = %q/%q, want %s/%s",
			settings.Provider,
			settings.Model,
			testProviderOpenAI,
			testGPT5ModelID,
		)
	}

	if settings.ThinkingLevel != string(model.ThinkingHigh) {
		t.Fatalf("thinking level = %q, want %q", settings.ThinkingLevel, model.ThinkingHigh)
	}

	if !slices.Equal(settings.ScopedEnabled, []string{}) {
		t.Fatalf("scoped enabled = %v, want []", settings.ScopedEnabled)
	}
}

func TestMoveScopedModelGuards(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.scopedOrder = []string{"a", "b"}

	if app.moveScopedModel("missing", 1) {
		t.Fatal("moveScopedModel should fail for missing value")
	}

	if app.moveScopedModel("a", -1) {
		t.Fatal("moveScopedModel should fail when target index is out of bounds")
	}
}

func TestHandleScopedModelKeyEnableAll(t *testing.T) {
	t.Parallel()

	app := newScopedModelTestApp(t)
	if !app.handleScopedModelKey(tcell.NewEventKey(tcell.KeyCtrlA, "", tcell.ModNone)) {
		t.Fatal("ctrl+a should be handled")
	}

	for _, item := range app.panel.FilteredItems() {
		if !app.scopedEnabled[item.Value] {
			t.Fatalf("expected scoped model %q to be enabled", item.Value)
		}
	}
}

func TestHandleScopedModelKeyClearAll(t *testing.T) {
	t.Parallel()

	app := newScopedModelTestApp(t)
	for _, item := range app.panel.FilteredItems() {
		app.scopedEnabled[item.Value] = true
	}

	if !app.handleScopedModelKey(tcell.NewEventKey(tcell.KeyCtrlX, "", tcell.ModNone)) {
		t.Fatal("ctrl+x should be handled")
	}

	for _, item := range app.panel.FilteredItems() {
		if app.scopedEnabled[item.Value] {
			t.Fatalf("expected scoped model %q to be cleared", item.Value)
		}
	}
}

func TestHandleScopedModelKeyToggleProvider(t *testing.T) {
	t.Parallel()

	app := newScopedModelTestApp(t)
	if !app.handleScopedModelKey(tcell.NewEventKey(tcell.KeyCtrlP, "", tcell.ModNone)) {
		t.Fatal("ctrl+p should be handled")
	}

	items := app.panel.FilteredItems()

	provider := providerFromModelValue(items[0].Value)
	for _, item := range items {
		if providerFromModelValue(item.Value) == provider && !app.scopedEnabled[item.Value] {
			t.Fatalf("expected provider-scoped model %q to be enabled", item.Value)
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

	if !app.handleScopedModelKey(tcell.NewEventKey(tcell.KeyUp, "", tcell.ModAlt)) {
		t.Fatal("alt+up should be handled")
	}

	if got := app.scopedOrder[0]; got != selected {
		t.Fatalf("scoped order[0] = %q, want %q after reorder", got, selected)
	}
}

func TestHandleScopedModelKeySave(t *testing.T) {
	t.Parallel()

	app := newScopedModelTestApp(t)
	if !app.handleScopedModelKey(tcell.NewEventKey(tcell.KeyCtrlS, "", tcell.ModNone)) {
		t.Fatal("ctrl+s should be handled")
	}

	if app.selectedPanelKind != "" || app.panel != nil {
		t.Fatal("save should close scoped models panel")
	}
}

func newScopedModelTestApp(t *testing.T) *App {
	t.Helper()

	app := newRenderTestApp(t)

	storage, err := auth.NewInMemoryStorage(context.Background(), map[string]auth.Credential{
		promptSendTestProvider: testPanelAuthCredential(),
	})
	if err != nil {
		t.Fatalf("create auth storage: %v", err)
	}

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

	if len(app.panel.FilteredItems()) < 2 {
		t.Fatal("expected at least two scoped model items")
	}

	return app
}
