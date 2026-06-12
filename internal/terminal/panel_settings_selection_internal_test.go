package terminal

import "testing"

func TestOpenAuxPanelsUseDedicatedKinds(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)

	app.openHotkeysPanel()
	if got, want := app.selectedPanelKind, panelHotkeys; got != want {
		t.Fatalf("hotkeys selectedPanelKind = %q, want %q", got, want)
	}
	if app.panel == nil || app.panel.Kind() != panelHotkeys {
		t.Fatal("hotkeys panel should use panelHotkeys kind")
	}

	app.openChangelogPanel()
	if got, want := app.selectedPanelKind, panelChangelog; got != want {
		t.Fatalf("changelog selectedPanelKind = %q, want %q", got, want)
	}
	if app.panel == nil || app.panel.Kind() != panelChangelog {
		t.Fatal("changelog panel should use panelChangelog kind")
	}
}
