//nolint:testpackage // These tests exercise unexported terminal markdown rendering helpers.
package terminal

import (
	"strings"
	"testing"
)

func TestRenderMarkdownListContinuationDoesNotRepeatBullet(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	lines := app.renderMarkdown("- alpha beta gamma delta epsilon zeta eta theta", 18)

	bulletLines := 0
	for _, line := range lines {
		if strings.Contains(line.Text, markdownBullet) {
			bulletLines++
		}
	}
	if bulletLines != 1 {
		t.Fatalf("bullet lines = %d, want 1; lines = %#v", bulletLines, lineTexts(lines))
	}
	if len(lines) < 2 {
		t.Fatalf("expected wrapped list item, got lines = %#v", lineTexts(lines))
	}
	if strings.Contains(lines[1].Text, markdownBullet) {
		t.Fatalf("continuation line repeated bullet: %q", lines[1].Text)
	}
}
