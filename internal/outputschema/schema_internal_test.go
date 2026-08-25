package outputschema

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

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
