//nolint:testpackage // These tests validate unexported scoped model panel helpers directly.
package terminal

import "testing"

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
