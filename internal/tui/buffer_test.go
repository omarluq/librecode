package tui_test

import (
	"testing"

	"github.com/gdamore/tcell/v3"
	cellcolor "github.com/gdamore/tcell/v3/color"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/tui"
)

func TestCellBufferEqualCloneAndCombiningRunes(t *testing.T) {
	t.Parallel()

	style := tcell.StyleDefault.Foreground(cellcolor.Blue)
	first := tui.Cell{Rune: 'e', Comb: []rune{'\u0301'}, Style: style}
	second := tui.Cell{Rune: 'e', Comb: []rune{'\u0301'}, Style: style}
	require.True(t, first.Equal(second))
	require.False(t, first.Equal(tui.Cell{Rune: 'e', Comb: nil, Style: style}))

	buffer := tui.NewCellBuffer(-10, -1, style)
	require.Zero(t, buffer.Width())
	require.Zero(t, buffer.Height())

	buffer = tui.NewCellBuffer(1, 1, style)
	combining := []rune{'\u0301'}
	buffer.SetContent(0, 0, 'e', combining, style)
	combining[0] = 'x'
	clone := buffer.Clone()
	buffer.SetContent(0, 0, 'a', nil, style)
	require.Equal(t, []rune{'\u0301'}, clone.Cell(0, 0).Comb)
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
