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
	if got := app.theme.name; got != "light" {
		t.Fatalf("theme after toggle = %q, want light", got)
	}

	app.toggleTheme()
	if got := app.theme.name; got != "dark" {
		t.Fatalf("theme after second toggle = %q, want dark", got)
	}
}
