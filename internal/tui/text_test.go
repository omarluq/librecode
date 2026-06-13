package tui_test

import (
	"testing"

	"github.com/gdamore/tcell/v3"
	cellcolor "github.com/gdamore/tcell/v3/color"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/tui"
)

const textBeta = "beta"

func TestLineHelpers(t *testing.T) {
	t.Parallel()

	line := tui.NewLine(tcell.StyleDefault.Bold(true), "hello")
	require.Equal(t, "hello", line.Text)
	assert.Empty(t, line.Spans)

	assert.Equal(
		t,
		[]tui.Line{testLine("b"), testLine("c")},
		tui.Tail([]tui.Line{testLine("a"), testLine("b"), testLine("c")}, 2),
	)
	assert.Equal(t, []tui.Line{}, tui.Tail([]tui.Line{testLine("a")}, 0))
	assert.Equal(t, []tui.Line{testLine("a")}, tui.Tail([]tui.Line{testLine("a")}, 5))
}

func TestBordersAndBufferDimensions(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 1, tui.RuneLen("語"))
	assert.Equal(t, "╭─title──╮", tui.TopBorder(10, "title"))
	assert.Equal(t, 10, tui.Width(tui.TopBorder(10, "語")))
	assert.Equal(t, "├────────┤", tui.MiddleBorder(10))
	assert.Equal(t, "╰────────╯", tui.BottomBorder(10))
	assert.Equal(t, "╭", tui.TopBorder(1, "ignored"))

	buffer := tui.NewCellBuffer(3, 2, tcell.StyleDefault)
	assert.Equal(t, 3, buffer.Width())
	assert.Equal(t, 2, buffer.Height())
	buffer.SetContent(-1, 0, 'x', nil, tcell.StyleDefault)
	buffer.SetContent(1, 1, 'y', nil, tcell.StyleDefault)
	assert.Equal(t, 'y', buffer.Cell(1, 1).Rune)
}

func TestRendererFlushWritesOnlyChangedCells(t *testing.T) {
	t.Parallel()

	screen := &recordingScreen{cells: map[[2]int]rune{}}
	style := tcell.StyleDefault
	current := tui.NewCellBuffer(2, 1, style)
	current.SetContent(1, 0, 'x', nil, style)

	renderer := tui.NewRenderer(screen)
	renderer.Flush(current)

	assert.Equal(t, 'x', screen.cells[[2]int{1, 0}])
}

func TestTextEdgeCases(t *testing.T) {
	t.Parallel()

	assert.Empty(t, tui.Truncate("hello", 0))
	assert.Equal(t, "hello", tui.Truncate("hello", 10))
	assert.Empty(t, tui.Fit("hello", 0))
	assert.Equal(t, "hello  ", tui.PadRight("hello", 7))
	assert.Equal(t, " ", tui.PadRight("語", 1))

	segments := tui.Segments("alpha beta")
	breakIndex := tui.WrapBreakIndex(segments, 20)
	assert.Equal(t, len(segments), breakIndex)
	assert.Equal(t, []string{"alph", "a"}, tui.Wrap("alpha", 4))
	assert.Equal(t, []string{"alpha", textBeta}, tui.Wrap("alpha  beta", 6))
}

func TestWriteCellsNoFillAndSegmentBounds(t *testing.T) {
	t.Parallel()

	buffer := tui.NewCellBuffer(4, 1, tcell.StyleDefault)
	used := tui.WriteCellsNoFill(buffer, 0, 0, 4, "語x", tcell.StyleDefault)
	assert.Equal(t, 3, used)
	assert.Equal(t, '語', buffer.Cell(0, 0).Rune)
	assert.Equal(t, ' ', buffer.Cell(1, 0).Rune)
	assert.Equal(t, 'x', buffer.Cell(2, 0).Rune)

	used = tui.WriteCellsNoFill(buffer, 0, 0, 1, "語", tcell.StyleDefault)
	assert.Equal(t, 0, used)

	used = tui.WriteCellsNoFill(buffer, 0, 0, 4, "a\u0301", tcell.StyleDefault)
	assert.Equal(t, 1, used)
}

func TestTextWidthFitTruncateAndPad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		text      string
		fit       string
		truncate  string
		padRight  string
		width     int
		textWidth int
	}{
		{
			name:      "ascii",
			text:      "hello",
			fit:       "hell",
			truncate:  "hel…",
			padRight:  "hell",
			width:     4,
			textWidth: 5,
		},
		{
			name:      "wide rune",
			text:      "語x",
			fit:       "語",
			truncate:  "…",
			padRight:  "語",
			width:     2,
			textWidth: 3,
		},
		{
			name:      "combining mark",
			text:      "e\u0301x",
			fit:       "e\u0301",
			truncate:  "…",
			padRight:  "e\u0301",
			width:     1,
			textWidth: 2,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.textWidth, tui.Width(testCase.text))
			assert.Equal(t, testCase.fit, tui.Fit(testCase.text, testCase.width))
			assert.Equal(t, testCase.truncate, tui.Truncate(testCase.text, testCase.width))
			assert.Equal(t, testCase.padRight, tui.PadRight(testCase.text, testCase.width))
		})
	}
}

func TestWrapModes(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"alpha", textBeta}, tui.Wrap("alpha beta", 6))
	assert.Equal(t, []string{"alpha ", textBeta}, tui.WrapPreserveWhitespace("alpha beta", 6))
	assert.Equal(t, []string{""}, tui.Wrap("anything", 0))
	assert.Equal(t, []string{"one", "two"}, tui.Wrap("one\ntwo", 10))
}

func TestWriteCellsHandlesWideRunesTabsAndFill(t *testing.T) {
	t.Parallel()

	buffer := tui.NewCellBuffer(8, 1, tcell.StyleDefault)
	used := tui.WriteCells(buffer, 0, 0, 8, "語\tx", tcell.StyleDefault.Bold(true))

	assert.Equal(t, 8, used)
	assert.Equal(t, '語', buffer.Cell(0, 0).Rune)
	assert.Equal(t, ' ', buffer.Cell(1, 0).Rune)
	assert.Equal(t, ' ', buffer.Cell(2, 0).Rune)
	assert.Equal(t, ' ', buffer.Cell(5, 0).Rune)
	assert.Equal(t, 'x', buffer.Cell(6, 0).Rune)
	assert.Equal(t, ' ', buffer.Cell(7, 0).Rune)
}

func TestBufferClone(t *testing.T) {
	t.Parallel()

	style := tcell.StyleDefault.Foreground(cellcolor.Red)
	buffer := tui.NewCellBuffer(2, 1, style)
	buffer.SetContent(0, 0, 'a', nil, style)
	clone := buffer.Clone()
	buffer.SetContent(0, 0, 'b', nil, style)

	assert.Equal(t, 'a', clone.Cell(0, 0).Rune)
	assert.Equal(t, 'b', buffer.Cell(0, 0).Rune)
}

func testLine(text string) tui.Line {
	return tui.Line{Style: tcell.StyleDefault, Text: text, Spans: nil}
}

type recordingScreen struct {
	cells map[[2]int]rune
}

func (screen *recordingScreen) SetContent(x, y int, primary rune, _ []rune, _ tcell.Style) {
	screen.cells[[2]int{x, y}] = primary
}
