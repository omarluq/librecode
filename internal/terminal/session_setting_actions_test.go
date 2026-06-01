//nolint:testpackage // These tests exercise unexported terminal toggle helpers.
package terminal

import (
	"context"
	"encoding/json"
	"testing"

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
	for _, value := range settings.ScopedEnabled {
		if value == testAnthropicClaudeID {
			t.Fatalf("unexpected cleared scoped model in persisted settings: %q", value)
		}
	}
}
