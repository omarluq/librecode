//nolint:testpackage // These tests validate unexported panel model helpers directly.
package terminal

import "testing"

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
