//nolint:testpackage // These tests validate unexported settings panel helpers directly.
package terminal

import "testing"

func TestSettingsItems(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	items := app.settingsItems()
	if got, want := len(items), 4; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	if got, want := items[0].Value, "theme"; got != want {
		t.Fatalf("items[0].Value = %q, want %q", got, want)
	}
}

func TestToggleTheme(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	if got := app.theme.name; got != "dark" {
		t.Fatalf("initial theme = %q, want dark", got)
	}

	app.toggleTheme()
	if got := app.theme.name; got != themeNameLight {
		t.Fatalf("theme after toggle = %q, want %s", got, themeNameLight)
	}

	app.toggleTheme()
	if got := app.theme.name; got != "dark" {
		t.Fatalf("theme after second toggle = %q, want dark", got)
	}
}

func TestApplySettingSelectionRefreshesPanel(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.openSettingsPanel()
	if app.panel == nil {
		t.Fatal("settings panel should be open")
	}

	app.applySettingSelection(settingTheme)
	if got, want := app.theme.name, themeNameLight; got != want {
		t.Fatalf("theme after setting selection = %q, want %q", got, want)
	}
	if app.sessionID != "" {
		t.Fatal("render test app should not persist theme settings without a session")
	}
	if app.panel == nil || app.panel.Kind() != panelSettings {
		t.Fatal("settings panel should be rebuilt after selection")
	}

	app.applySettingSelection("tools-expanded")
	if !app.toolsExpanded {
		t.Fatal("toolsExpanded should toggle on selection")
	}
	if app.panel == nil || app.panel.Kind() != panelSettings {
		t.Fatal("settings panel should remain open after tools toggle")
	}
}
