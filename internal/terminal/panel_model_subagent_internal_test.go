package terminal

import (
	"testing"

	"github.com/omarluq/librecode/internal/auth"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/testutil"
)

func TestModelSubagentPanelSelection(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.cfg = promptSendTestConfig()

	storage := testutil.NewAuthStorage(t, map[string]auth.Credential{
		promptSendTestProvider: testPanelAuthCredential(),
	})

	app.models = model.NewRegistry(&model.RegistryOptions{
		ConfigReader: nil,
		Auth:         storage,
		ModelsPath:   "",
		BuiltIns: []model.Model{
			newPanelTestModel(promptSendTestModel, "Current"),
			newPanelTestModel("other-model", "Other"),
		},
		Discovery: disabledModelDiscovery(),
	})

	app.openModelSubagentPanel()

	if got, want := app.selectedPanelKind, panelModelSubagent; got != want {
		t.Fatalf("selectedPanelKind = %q, want %q", got, want)
	}

	if app.panel == nil || app.panel.Kind != panelModelSubagent {
		t.Fatal("subagent model panel should be open")
	}

	if items := app.panel.Items(); len(items) != 2 {
		t.Fatalf("len(panel.items) = %d, want 2", len(items))
	}

	app.applyModelSubagentSelection(promptSendTestProvider + "/other-model")

	if got, want := app.currentDelegationModel(), "other-model"; got != want {
		t.Fatalf("currentDelegationModel = %q, want %q", got, want)
	}

	if got, want := app.currentDelegationProvider(), promptSendTestProvider; got != want {
		t.Fatalf("currentDelegationProvider = %q, want %q", got, want)
	}

	if got, want := app.currentModel(), promptSendTestModel; got != want {
		t.Fatalf("assistant currentModel = %q, want %q (unchanged)", got, want)
	}
}
