//nolint:testpackage // These tests validate unexported panel model helpers directly.
package terminal

import (
	"context"
	"testing"

	"github.com/omarluq/librecode/internal/auth"
	"github.com/omarluq/librecode/internal/model"
)

func TestEnsureCurrentModel(t *testing.T) {
	t.Parallel()

	const testGPT5Model = "gpt-5"

	models := ensureCurrentModel(nil, testProviderOpenAI, testGPT5Model)
	if got, want := len(models), 1; got != want {
		t.Fatalf("len(models) = %d, want %d", got, want)
	}
	if got, want := models[0].Provider, testProviderOpenAI; got != want {
		t.Fatalf("models[0].Provider = %q, want %q", got, want)
	}
	if got, want := models[0].ID, testGPT5Model; got != want {
		t.Fatalf("models[0].ID = %q, want %q", got, want)
	}

	models = ensureCurrentModel(models, promptSendTestProvider, promptSendTestModel)
	if got, want := len(models), 2; got != want {
		t.Fatalf("len(models) after second call = %d, want %d", got, want)
	}
	if got, want := models[1].Provider, promptSendTestProvider; got != want {
		t.Fatalf("models[1].Provider = %q, want %q", got, want)
	}
}

func TestModelPanelSelectionAndCycling(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.cfg = promptSendTestConfig()
	storage, err := auth.NewInMemoryStorage(context.Background(), map[string]auth.Credential{
		promptSendTestProvider: testPanelAuthCredential(),
	})
	if err != nil {
		t.Fatalf("create auth storage: %v", err)
	}
	app.models = model.NewRegistry(&model.RegistryOptions{
		ConfigSource: nil,
		Auth:         storage,
		ModelsPath:   "",
		BuiltIns: []model.Model{
			newPanelTestModel(promptSendTestModel, "Current"),
			newPanelTestModel("other-model", "Other"),
		},
		Discovery: disabledModelDiscovery(),
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
	if app.sessionID != "" {
		t.Fatal("render test app should not persist model settings without a session")
	}

	app.cycleModel(1)
	if got, want := app.currentModel(), promptSendTestModel; got != want {
		t.Fatalf("currentModel after cycle = %q, want %q", got, want)
	}
	if values := app.cycleModelValues(); len(values) != 2 {
		t.Fatalf("len(cycleModelValues) = %d, want 2", len(values))
	}
}

func TestAvailableModelsDoesNotFallbackToUnauthorizedCatalog(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.cfg = promptSendTestConfig()
	app.models = model.NewRegistry(&model.RegistryOptions{
		ConfigSource: nil,
		Auth:         nil,
		ModelsPath:   "",
		BuiltIns: []model.Model{
			newPanelTestModel(promptSendTestModel, "Current"),
			newPanelTestModel("other-model", "Other"),
		},
		Discovery: disabledModelDiscovery(),
	})

	models := app.availableModels()
	if len(models) != 1 {
		t.Fatalf("len(availableModels) = %d, want current model only", len(models))
	}
	if got, want := models[0].ID, promptSendTestModel; got != want {
		t.Fatalf("availableModels[0].ID = %q, want %q", got, want)
	}
}

func testPanelAuthCredential() auth.Credential {
	return auth.Credential{
		OAuth:     nil,
		Type:      auth.CredentialTypeAPIKey,
		Key:       "test-key",
		Access:    "",
		Refresh:   "",
		AccountID: "",
		Expires:   0,
		ExpiresAt: 0,
	}
}

func newPanelTestModel(modelID, name string) model.Model {
	return model.Model{
		ThinkingLevelMap: nil,
		Headers:          nil,
		Compat:           nil,
		Provider:         promptSendTestProvider,
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
