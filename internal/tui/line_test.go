package tui_test

import (
	"testing"

	"github.com/gdamore/tcell/v3"
	cellcolor "github.com/gdamore/tcell/v3/color"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/tui"
)

func TestLineStyledOperationsPreserveSpans(t *testing.T) {
	t.Parallel()

	red := tcell.StyleDefault.Foreground(cellcolor.Red)
	green := tcell.StyleDefault.Foreground(cellcolor.Green)
	line := tui.Line{
		Text:  testHello + " world",
		Style: tcell.StyleDefault,
		Spans: []tui.Span{{Text: testHello, Style: red}, {Text: " world", Style: green}},
	}

	clone := line.Clone()
	clone.Spans[0].Text = "changed"
	require.Equal(t, testHello, line.Spans[0].Text)

	truncated := line.Truncate(7)
	require.Equal(t, "hello …", truncated.Text)
	require.Len(t, truncated.Spans, 2)
	require.Equal(t, green, truncated.Spans[1].Style)

	require.Equal(t, []string{testHello, "world"}, lineTexts(line.Wrap(6)))
	require.Equal(t, []string{"hello ", "world"}, lineTexts(line.WrapPreserveWhitespace(6)))
	require.Equal(t, []string{testHello, " worl", "d"}, lineTexts(line.WrapCells(5)))

	prefixed := line.WithPrefix("› ", red)
	require.Equal(t, "› hello world", prefixed.Text)
	require.Equal(t, "› ", prefixed.Spans[0].Text)
	require.Equal(t, red, prefixed.Spans[0].Style)
}

func TestLinePlainTextAndEdgeCases(t *testing.T) {
	t.Parallel()

	plain := tui.NewLine(tcell.StyleDefault, testAlpha+" "+testBeta)
	require.Equal(t, []string{testAlpha, testBeta}, lineTexts(plain.Wrap(6)))
	require.Equal(t, []string{testAlpha + " ", testBeta}, lineTexts(plain.WrapPreserveWhitespace(6)))
	require.Equal(t, []string{""}, lineTexts(plain.Wrap(0)))
	require.Equal(t, []string{""}, lineTexts(plain.WrapCells(0)))

	wide := tui.NewLine(tcell.StyleDefault, "語x")
	require.Equal(t, "…", wide.Truncate(1).Text)
	require.Equal(t, "語x", wide.Truncate(3).Text)

	styled := tui.Line{
		Text:  " ab",
		Style: tcell.StyleDefault,
		Spans: []tui.Span{{Text: " ", Style: tcell.StyleDefault}, {Text: "ab", Style: tcell.StyleDefault}},
	}
	require.Equal(t, []string{" a", "b"}, lineTexts(styled.Wrap(2)))
}

func TestLineHelpers(t *testing.T) {
	t.Parallel()

	line := tui.NewLine(tcell.StyleDefault.Bold(true), "hello")
	require.Equal(t, "hello", line.Text)
	require.Empty(t, line.Spans)

	require.Equal(
		t,
		[]tui.Line{testLine("b"), testLine("c")},
		tui.Tail([]tui.Line{testLine("a"), testLine("b"), testLine("c")}, 2),
	)
	require.Equal(t, []tui.Line{}, tui.Tail([]tui.Line{testLine("a")}, 0))
	require.Equal(t, []tui.Line{testLine("a")}, tui.Tail([]tui.Line{testLine("a")}, 5))
}

func TestWrapLines(t *testing.T) {
	t.Parallel()

	lines := []tui.Line{tui.NewLine(tcell.StyleDefault, testAlpha+" "+testBeta)}
	require.Equal(t, []string{testAlpha, testBeta}, lineTexts(tui.WrapLines(lines, 6)))
	require.Empty(t, tui.WrapLines(lines, 0))
}
