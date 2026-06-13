package terminal

import (
	"testing"

	"github.com/omarluq/librecode/internal/model"
)

func TestScopedHelpers(t *testing.T) {
	t.Parallel()

	if got, want := scopedModelIndex([]string{"a", "b", "c"}, "b"), 1; got != want {
		t.Fatalf("scopedModelIndex = %d, want %d", got, want)
	}

	if got, want := scopedModelIndex([]string{"a"}, "z"), -1; got != want {
		t.Fatalf("scopedModelIndex missing = %d, want %d", got, want)
	}

	if got, want := providerFromModelValue("openai/gpt-5"), testProviderOpenAI; got != want {
		t.Fatalf("providerFromModelValue = %q, want %q", got, want)
	}

	if got, want := providerFromModelValue("gpt-5"), ""; got != want {
		t.Fatalf("providerFromModelValue missing provider = %q, want %q", got, want)
	}
}

func TestScopedModelPanelBehavior(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.models = model.NewRegistry(nil)
	app.scopedEnabled = map[string]bool{}

	app.openScopedModelsPanel()

	if got, want := app.selectedPanelKind, panelScopedModels; got != want {
		t.Fatalf("selectedPanelKind = %q, want %q", got, want)
	}

	if app.panel == nil || app.panel.Kind() != panelScopedModels {
		t.Fatal("scoped models panel should be open")
	}

	items := app.panel.Items()
	if len(items) == 0 {
		t.Fatal("scoped models panel should include model items")
	}

	value := items[0].Value
	app.toggleScopedModel(value)

	if !app.scopedEnabled[value] {
		t.Fatalf("scopedEnabled[%q] = false, want true", value)
	}

	if app.sessionID != "" {
		t.Fatal("render test app should not persist scoped model settings without a session")
	}

	if app.panel == nil || app.panel.Kind() != panelScopedModels {
		t.Fatal("scoped models panel should remain open after toggle")
	}

	if got, want := app.panel.Items()[0].Value, value; got != want {
		t.Fatalf("panel item value = %q, want %q", got, want)
	}
}
