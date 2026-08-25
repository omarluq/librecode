package outputschema

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestDenyURLLoaderNeverReadsExternalResources(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "schema.json")
	if err := os.WriteFile(path, []byte(`true`), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, resource := range []string{"file://" + path, "https://example.invalid/schema.json"} {
		value, err := (denyURLLoader{}).Load(resource)
		if err == nil || value != nil {
			t.Fatalf("Load(%q) = (%v, %v), want (nil, error)", resource, value, err)
		}
	}
}

func TestFailureDiagnosticTruncationPreservesUTF8(t *testing.T) {
	t.Parallel()

	err := failure("test", "%s", strings.Repeat("x", MaxDiagnosticBytes-1)+"é trailing text")

	var diagnostic *Error

	if !errors.As(err, &diagnostic) {
		t.Fatalf("failure type = %T", err)
	}

	if !utf8.ValidString(diagnostic.Reason) {
		t.Fatalf("diagnostic reason is not valid UTF-8: %q", diagnostic.Reason)
	}

	if len(diagnostic.Reason) > MaxDiagnosticBytes {
		t.Fatalf("diagnostic reason bytes = %d, want <= %d", len(diagnostic.Reason), MaxDiagnosticBytes)
	}

	if diagnostic.Reason != strings.Repeat("x", MaxDiagnosticBytes-1) {
		t.Fatalf("diagnostic was not truncated at the rune boundary")
	}
}
