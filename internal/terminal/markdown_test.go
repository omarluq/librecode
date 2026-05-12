//nolint:testpackage // These tests exercise unexported terminal markdown rendering helpers.
package terminal

import (
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2"
	"github.com/gdamore/tcell/v3"
)

func TestRenderMarkdownCodeBlockHighlightsSyntax(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	lines := app.renderMarkdown("```go\nfunc main() {\n\tfmt.Println(\"hi\")\n}\n```", 80)

	content := findLineContaining(t, lines, "func main")
	if len(content.Spans) == 0 {
		t.Fatalf("code line has no styled spans: %#v", content)
	}
	if !lineHasForeground(content, codeKeywordColor(app.theme)) {
		t.Fatalf("code line does not include keyword color; spans = %#v", content.Spans)
	}
	if !lineHasForeground(content, codeFunctionColor(app.theme)) {
		t.Fatalf("code line does not include function color; spans = %#v", content.Spans)
	}
}

func TestRenderMarkdownCodeBlockDoesNotRenderFrameBorders(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	lines := app.renderMarkdown("```go\nfunc main() {}\n```", 40)

	for _, line := range lines {
		if strings.ContainsAny(line.Text, "╭╮╰╯│") {
			t.Fatalf("code block rendered frame border: %q", line.Text)
		}
	}
}

func TestSyntaxHighlightFallsBackForUnknownLanguage(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	style := app.theme.background(colorCodeBg).Foreground(app.theme.colors[colorCodeText])
	lines := syntaxHighlightedCodeLines("not-a-real-language", "plain text", app.theme, style)

	if len(lines) != 1 {
		t.Fatalf("highlighted lines = %d, want 1", len(lines))
	}
	if len(lines[0].Spans) != 0 {
		t.Fatalf("unknown language should not create spans: %#v", lines[0].Spans)
	}
}

func TestStyleForTokenUsesItalicComments(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	style := styleForToken(chroma.CommentSingle, app.theme, app.theme.style(colorCodeText))
	if !style.HasItalic() {
		t.Fatal("comment style should be italic")
	}
}

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

func findLineContaining(t *testing.T, lines []styledLine, needle string) styledLine {
	t.Helper()
	for _, line := range lines {
		if strings.Contains(line.Text, needle) {
			return line
		}
	}
	t.Fatalf("line containing %q not found in %#v", needle, lineTexts(lines))

	return newStyledLine(tcell.StyleDefault, "")
}

func lineHasForeground(line styledLine, color tcell.Color) bool {
	for _, span := range line.Spans {
		if span.Style.GetForeground() == color {
			return true
		}
	}

	return false
}
