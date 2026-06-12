package rendertext_test

import (
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/terminal/rendertext"
)

func TestLineHelpers(t *testing.T) {
	t.Parallel()

	line := rendertext.NewLine(tcell.StyleDefault.Bold(true), "hello")
	require.Equal(t, "hello", line.Text)
	assert.Empty(t, line.Spans)

	assert.Equal(
		t,
		[]rendertext.Line{testLine("b"), testLine("c")},
		rendertext.SafeTail([]rendertext.Line{testLine("a"), testLine("b"), testLine("c")}, 2),
	)
	assert.Equal(t, []rendertext.Line{}, rendertext.SafeTail([]rendertext.Line{testLine("a")}, 0))
	assert.Equal(t, []rendertext.Line{testLine("a")}, rendertext.SafeTail([]rendertext.Line{testLine("a")}, 5))
}

func TestBordersAndBufferDimensions(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 1, rendertext.RuneLen("語"))
	assert.Equal(t, "╭─title──╮", rendertext.TopBorder(10, "title"))
	assert.Equal(t, 10, rendertext.Width(rendertext.TopBorder(10, "語")))
	assert.Equal(t, "├────────┤", rendertext.MiddleBorder(10))
	assert.Equal(t, "╰────────╯", rendertext.BottomBorder(10))
	assert.Equal(t, "╭…╮", rendertext.TopBorder(1, "ignored"))

	buffer := rendertext.NewBuffer(3, 2, tcell.StyleDefault)
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
	current := rendertext.NewBuffer(2, 1, style)
	current.SetContent(1, 0, 'x', nil, style)

	renderer := rendertext.NewRenderer(screen)
	renderer.Flush(current)

	assert.Equal(t, 'x', screen.cells[[2]int{1, 0}])
}

func TestTextEdgeCases(t *testing.T) {
	t.Parallel()

	assert.Empty(t, rendertext.Truncate("hello", 0))
	assert.Equal(t, "hello", rendertext.Truncate("hello", 10))
	assert.Empty(t, rendertext.Fit("hello", 0))
	assert.Equal(t, "hello  ", rendertext.PadRight("hello", 7))
	assert.Equal(t, " ", rendertext.PadRight("語", 1))

	segments := rendertext.Segments("alpha beta")
	breakIndex := rendertext.WrapBreakIndex(segments, 20)
	assert.Equal(t, len(segments), breakIndex)
	assert.Equal(t, []string{"alph", "a"}, rendertext.Wrap("alpha", 4))
	assert.Equal(t, []string{"alpha", rendertextBeta}, rendertext.Wrap("alpha  beta", 6))
}

func TestWriteCellsNoFillAndSegmentBounds(t *testing.T) {
	t.Parallel()

	buffer := rendertext.NewBuffer(4, 1, tcell.StyleDefault)
	used := rendertext.WriteCellsNoFill(buffer, 0, 0, 4, "語x", tcell.StyleDefault)
	assert.Equal(t, 3, used)
	assert.Equal(t, '語', buffer.Cell(0, 0).Rune)
	assert.Equal(t, ' ', buffer.Cell(1, 0).Rune)
	assert.Equal(t, 'x', buffer.Cell(2, 0).Rune)

	used = rendertext.WriteCellsNoFill(buffer, 0, 0, 1, "語", tcell.StyleDefault)
	assert.Equal(t, 0, used)

	used = rendertext.WriteCellsNoFill(buffer, 0, 0, 4, "a\u0301", tcell.StyleDefault)
	assert.Equal(t, 1, used)
}
