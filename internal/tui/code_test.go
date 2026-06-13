package tui_test

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v3"
	cellcolor "github.com/gdamore/tcell/v3/color"

	"github.com/omarluq/librecode/internal/tui"
)

func TestCodeBlockWrapsInsteadOfSwallowingSymbols(t *testing.T) {
	t.Parallel()

	closeIndentedBlock := "    }"
	text := strings.Join([]string{
		"func FibSeq(n int) []int {",
		"    if n <= 1 {",
		"        return nil",
		closeIndentedBlock,
		"    seq := make([]int, n)",
		"    if n > 0 {",
		"        seq[0] = 0",
		closeIndentedBlock,
		"    if n > 1 {",
		"        seq[1] = 1",
		closeIndentedBlock,
		"    for i := 2; i < n; i++ {",
		"        seq[i] = seq[i-1] + seq[i-2]",
		closeIndentedBlock,
		"    return seq",
		"}",
	}, "\n")
	block := tui.CodeBlock{
		Language: "go",
		Text:     text,
		Theme:    testCodeTheme(),
		Style:    tcell.StyleDefault,
	}

	lines := block.Render(14, 100)
	reconstructed := strings.Join(strings.Fields(lineText(lines)), " ")

	assertContains(t, reconstructed, "if n <= 1 {")
	assertContains(t, reconstructed, "return nil")
	assertContains(t, reconstructed, "seq := make([]int, n)")
	assertContains(t, reconstructed, "if n > 0 {")
	assertContains(t, reconstructed, "if n > 1 {")
	assertContains(t, reconstructed, "i < n")
	assertContains(t, reconstructed, "seq[i-1] + seq[i-2]")
	assertContains(t, reconstructed, "return seq")
	assertNoLineWiderThan(t, lines, 14)
}

func TestMarkdownCodeBlockWrapsInsteadOfSwallowingSymbols(t *testing.T) {
	t.Parallel()

	markdown := "```go\nfunc Fib(n int) int {\n    if n < 2 {\n        return n\n    }\n}\n```"
	view := tui.MarkdownView{
		Text: markdown,
		Styles: tui.MarkdownStyles{
			Text:      tcell.StyleDefault,
			Accent:    tcell.StyleDefault,
			Muted:     tcell.StyleDefault,
			Code:      tcell.StyleDefault,
			CodeTheme: testCodeTheme(),
		},
	}

	lines := view.Render(20, 100)
	joined := lineText(lines)

	assertContains(t, joined, "<")
	assertContains(t, joined, "2")
	assertContains(t, joined, "{")
	assertContains(t, joined, "return n")
	assertNoLineWiderThan(t, lines, 20)
}

func TestSyntaxHighlightingUsesExpectedTokenStyles(t *testing.T) {
	t.Parallel()

	theme := testCodeTheme()
	lines := tui.SyntaxHighlightedCodeLines(
		"go",
		"func Fib(n int) []int {\n\treturn n + 1\n}",
		theme,
		tcell.StyleDefault,
	)

	assertSpanForeground(t, lines, "func", theme.Accent)
	assertSpanForeground(t, lines, "Fib", theme.Success)
	assertSpanForeground(t, lines, " ", tcell.ColorDefault)
	assertSpanForeground(t, lines, "[]", theme.Dim)
	assertSpanForeground(t, lines, "int", theme.Accent)
	assertSpanForeground(t, lines, "+", theme.Dim)
	assertSpanForeground(t, lines, "1", theme.Text)
}

func lineText(lines []tui.Line) string {
	texts := make([]string, 0, len(lines))
	for _, line := range lines {
		texts = append(texts, line.Text)
	}

	return strings.Join(texts, "\n")
}

func assertContains(t *testing.T, text, want string) {
	t.Helper()

	if !strings.Contains(text, want) {
		t.Fatalf("rendered text missing %q:\n%s", want, text)
	}
}

func assertSpanForeground(t *testing.T, lines []tui.Line, text string, want tcell.Color) {
	t.Helper()

	for _, line := range lines {
		for _, span := range line.Spans {
			if span.Text == text {
				if got := span.Style.GetForeground(); got != want {
					t.Fatalf("span %q foreground = %v, want %v", text, got, want)
				}

				return
			}
		}
	}

	t.Fatalf("span %q not found in lines: %#v", text, lines)
}

func assertNoLineWiderThan(t *testing.T, lines []tui.Line, width int) {
	t.Helper()

	for _, line := range lines {
		if line.Width() > width {
			t.Fatalf("line width = %d, want <= %d: %q", line.Width(), width, line.Text)
		}
	}
}

func testCodeTheme() tui.CodeTheme {
	return tui.CodeTheme{
		Text:    cellcolor.White,
		Accent:  cellcolor.Blue,
		Success: cellcolor.Green,
		Warning: cellcolor.Yellow,
		Dim:     cellcolor.Gray,
		Muted:   cellcolor.Gray,
		DiffAdd: cellcolor.Green,
		DiffDel: cellcolor.Red,
	}
}
