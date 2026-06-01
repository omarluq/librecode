//nolint:testpackage // These tests validate unexported panel model helpers directly.
package terminal

import (
	"testing"

	"github.com/omarluq/librecode/internal/model"
)

func TestEnsureCurrentModel(t *testing.T) {
	t.Parallel()

	models := ensureCurrentModel(nil, "openai", "gpt-5")
	if got, want := len(models), 1; got != want {
		t.Fatalf("len(models) = %d, want %d", got, want)
	}
	if got, want := models[0].Provider, testProviderOpenAI; got != want {
		t.Fatalf("models[0].Provider = %q, want %q", got, want)
	}
	if got, want := models[0].ID, "gpt-5"; got != want {
		t.Fatalf("models[0].ID = %q, want %q", got, want)
	}
}

func TestModelPanelSelectionAndCycling(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.cfg = promptSendTestConfig()
	app.models = model.NewRegistry(&model.RegistryOptions{
		ConfigSource: nil,
		Auth:         nil,
		ModelsPath:   "",
		BuiltIns: []model.Model{
			newPanelTestModel(promptSendTestProvider, promptSendTestModel, "Current"),
			newPanelTestModel(promptSendTestProvider, "other-model", "Other"),
		},
	})

	app.openModelPanel()
	if got, want := app.selectedPanelKind, panelModel; got != want {
		t.Fatalf("selectedPanelKind = %q, want %q", got, want)
	}
	if app.panel == nil || app.panel.kind != panelModel {
		t.Fatal("model panel should be open")
	}
	if len(app.panel.items) != 2 {
		t.Fatalf("len(panel.items) = %d, want 2", len(app.panel.items))
	}

	app.applyModelSelection(promptSendTestProvider + "/other-model")
	if got, want := app.currentModel(), "other-model"; got != want {
		t.Fatalf("currentModel = %q, want %q", got, want)
	}

	app.cycleModel(1)
	if got, want := app.currentModel(), promptSendTestModel; got != want {
		t.Fatalf("currentModel after cycle = %q, want %q", got, want)
	}
	if values := app.cycleModelValues(); len(values) != 2 {
		t.Fatalf("len(cycleModelValues) = %d, want 2", len(values))
	}
}

func newPanelTestModel(provider, modelID, name string) model.Model {
	return model.Model{
		ThinkingLevelMap: nil,
		Headers:          nil,
		Compat:           nil,
		Provider:         provider,
		ID:               modelID,
		Name:             name,
		API:              "",
		BaseURL:          "",
		Input:            []model.InputMode{model.InputText},
		Cost: model.Cost{
			Input:      0,
			Output:     0,
			CacheRead:  0,
			CacheWrite: 0,
		},
		ContextWindow: 0,
		MaxTokens:     0,
		Reasoning:     false,
	}
}
