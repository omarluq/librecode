package rendertext_test

import (
	"testing"

	"github.com/gdamore/tcell/v3"
	cellcolor "github.com/gdamore/tcell/v3/color"
	"github.com/omarluq/librecode/internal/terminal/rendertext"
	"github.com/stretchr/testify/assert"
)

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

			assert.Equal(t, testCase.textWidth, rendertext.Width(testCase.text))
			assert.Equal(t, testCase.fit, rendertext.Fit(testCase.text, testCase.width))
			assert.Equal(t, testCase.truncate, rendertext.Truncate(testCase.text, testCase.width))
			assert.Equal(t, testCase.padRight, rendertext.PadRight(testCase.text, testCase.width))
		})
	}
}

const rendertextBeta = "beta"

func TestWrapModes(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"alpha", rendertextBeta}, rendertext.Wrap("alpha beta", 6))
	assert.Equal(t, []string{"alpha ", rendertextBeta}, rendertext.WrapPreserveWhitespace("alpha beta", 6))
	assert.Equal(t, []string{""}, rendertext.Wrap("anything", 0))
	assert.Equal(t, []string{"one", "two"}, rendertext.Wrap("one\ntwo", 10))
}

func TestWriteCellsHandlesWideRunesTabsAndFill(t *testing.T) {
	t.Parallel()

	buffer := rendertext.NewBuffer(8, 1, tcell.StyleDefault)
	used := rendertext.WriteCells(buffer, 0, 0, 8, "語\tx", tcell.StyleDefault.Bold(true))

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
	buffer := rendertext.NewBuffer(2, 1, style)
	buffer.SetContent(0, 0, 'a', nil, style)
	clone := buffer.Clone()
	buffer.SetContent(0, 0, 'b', nil, style)

	assert.Equal(t, 'a', clone.Cell(0, 0).Rune)
	assert.Equal(t, 'b', buffer.Cell(0, 0).Rune)
}
