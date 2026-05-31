//nolint:testpackage // These tests validate unexported tree panel helpers directly.
package terminal

import "testing"

func TestTreeDescription(t *testing.T) {
	t.Parallel()

	entry := testEntryEntity()
	entry.Message.Content = "hello"
	if got, want := treeDescription(&entry), "hello"; got != want {
		t.Fatalf("treeDescription(content) = %q, want %q", got, want)
	}

	entry = testEntryEntity()
	entry.Summary = "summary"
	if got, want := treeDescription(&entry), "summary"; got != want {
		t.Fatalf("treeDescription(summary) = %q, want %q", got, want)
	}

	entry = testEntryEntity()
	entry.Message.Provider = testProviderOpenAI
	entry.Message.Model = "gpt-5"
	if got, want := treeDescription(&entry), "openai/gpt-5"; got != want {
		t.Fatalf("treeDescription(model) = %q, want %q", got, want)
	}
}

func TestEmptyParentID(t *testing.T) {
	t.Parallel()

	if got := emptyParentID(nil); got == nil || *got != "" {
		t.Fatal("emptyParentID(nil) should return pointer to empty string")
	}
	value := "parent"
	if got := emptyParentID(&value); got != &value {
		t.Fatal("emptyParentID should return original pointer when non-nil")
	}
}
