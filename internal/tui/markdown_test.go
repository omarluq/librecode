package tui_test

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/tui"
)

func TestMarkdownViewRendersCommonBlocks(t *testing.T) {
	t.Parallel()

	markdown := strings.Join([]string{
		"# Heading",
		"",
		"Paragraph with [link](https://example.com) and `code`.",
		"",
		"> quoted text",
		"",
		"- bullet item",
		"- second item",
		"",
		"1. ordered item",
		"2. next ordered",
		"",
		"---",
		"",
		"    indented code",
		"",
		"| Name | Count |",
		"| :--- | ----: |",
		"| Alpha | 10 |",
	}, "\n")
	styles := tui.MarkdownStyles{
		Text:      tcell.StyleDefault,
		Accent:    tcell.StyleDefault,
		Muted:     tcell.StyleDefault,
		Code:      tcell.StyleDefault,
		CodeTheme: testCodeTheme(),
	}
	view := &tui.MarkdownView{Text: markdown, Styles: styles, Engine: nil}

	lines := view.Render(40, 100)
	text := strings.Join(lineTexts(lines), "\n")
	require.Contains(t, text, "# Heading")
	require.Contains(t, text, "link")
	require.Contains(t, text, "(https://example.com)")
	require.Contains(t, text, "`code`")
	require.Contains(t, text, "┃ quoted text")
	require.Contains(t, text, "• bullet item")
	require.Contains(t, text, "1. ordered item")
	require.Contains(t, text, "────────")
	require.Contains(t, text, "indented code")
	require.Contains(t, text, "Alpha")

	buffer := tui.NewCellBuffer(40, 2, tcell.StyleDefault)
	(&tui.MarkdownView{Text: "# Heading", Styles: styles, Engine: nil}).Draw(buffer, testRect(0, 0, 40, 2))
	require.Equal(t, '#', buffer.Cell(1, 0).Rune)
}

func TestMarkdownCodeBlockWrapsInsteadOfSwallowingSymbols(t *testing.T) {
	t.Parallel()

	markdown := "```go\nfunc Fib(n int) int {\n    if n < 2 {\n        return n\n    }\n}\n```"
	styles := tui.MarkdownStyles{
		Text:      tcell.StyleDefault,
		Accent:    tcell.StyleDefault,
		Muted:     tcell.StyleDefault,
		Code:      tcell.StyleDefault,
		CodeTheme: testCodeTheme(),
	}
	view := tui.MarkdownView{
		Text:   markdown,
		Styles: styles,
		Engine: nil,
	}

	lines := view.Render(20, 100)
	joined := lineText(lines)

	assertContains(t, joined, "<")
	assertContains(t, joined, "2")
	assertContains(t, joined, "{")
	assertContains(t, joined, "return n")
	assertNoLineWiderThan(t, lines, 20)
}
