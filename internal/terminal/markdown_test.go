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

func TestRenderMarkdownTableUsesRichBoxDrawing(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	lines := app.renderMarkdown("| Name | Count |\n| :--- | ---: |\n| apples | 12 |\n| pears | 3 |", 80)
	texts := lineTexts(lines)

	assertLineContains(t, texts, "╭────────┬───────╮")
	assertLineContains(t, texts, "│ Name   │ Count │")
	assertLineContains(t, texts, "├────────┼───────┤")
	assertLineContains(t, texts, "│ apples │    12 │")
	assertLineContains(t, texts, "╰────────┴───────╯")
}

func TestRenderMarkdownTableStylesHeaderAndBorders(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	lines := app.renderMarkdown("| Name | Count |\n| --- | --- |\n| apples | 12 |", 80)
	header := findLineContaining(t, lines, "Name")

	if len(header.Spans) == 0 {
		t.Fatalf("table header has no styled spans: %#v", header)
	}
	if header.Spans[0].Text != markdownIndent ||
		header.Spans[0].Style.GetForeground() != app.theme.colors[colorBorderMuted] {
		t.Fatalf("indent span = %#v, want muted border", header.Spans[0])
	}
	if header.Spans[1].Text != "│" || header.Spans[1].Style.GetForeground() != app.theme.colors[colorBorderMuted] {
		t.Fatalf("first table border span = %#v, want muted border", header.Spans[1])
	}
	if !lineHasForeground(header, app.theme.colors[colorAccent]) {
		t.Fatalf("table header does not include accent style: %#v", header.Spans)
	}
}

func TestRenderMarkdownTableBordersAlignWithWideCells(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	lines := app.renderMarkdown("| 项目 | Count |\n| :--- | ---: |\n| apples | 12 |", 80)
	app.frame = newCellBuffer(80, len(lines), tcell.StyleDefault)
	for row, line := range lines {
		app.writeStyledLine(row, 80, line)
	}
	text := frameText(app.frame)

	if !strings.Contains(text, "│ 项 目    │ Count │") {
		t.Fatalf("wide table cell borders are not aligned, frame = %q", text)
	}
}

func assertLineContains(t *testing.T, lines []string, needle string) {
	t.Helper()
	for _, line := range lines {
		if strings.Contains(line, needle) {
			return
		}
	}
	t.Fatalf("line containing %q not found in %#v", needle, lines)
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
