//nolint:testpackage // These tests exercise unexported terminal editor helpers.
package terminal

import "testing"

func TestEditorCursorPositionCountsTrailingSpaces(t *testing.T) {
	t.Parallel()

	row, column := editorCursorPosition([]rune("abc   "), 6, 20)
	if row != 0 {
		t.Fatalf("row = %d, want 0", row)
	}
	if column != 6 {
		t.Fatalf("column = %d, want 6", column)
	}
}

func TestEditorBodyLinesPreserveTrailingSpaces(t *testing.T) {
	t.Parallel()

	lines := editorBodyLines([]rune("abc   "), 20)
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}
	if got, want := lines[0], "abc   "; got != want {
		t.Fatalf("line = %q, want %q", got, want)
	}
}

func TestEditorCursorPositionUsesCellWidth(t *testing.T) {
	t.Parallel()

	_, column := editorCursorPosition([]rune("語 "), 2, 20)
	if column != 3 {
		t.Fatalf("column = %d, want 3", column)
	}
}
