package tui_test

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/tui"
)

func TestTableRenderAlignmentClippingAndDraw(t *testing.T) {
	t.Parallel()

	table := &tui.Table{
		Style:       tcell.StyleDefault,
		HeaderStyle: tcell.StyleDefault,
		BorderStyle: tcell.StyleDefault,
		Headers:     []tui.TableCell{testTableCell("Name"), testTableCell("Count")},
		Rows: [][]tui.TableCell{
			{testTableCell(testAlpha), testTableCell("1")},
			{testTableCell("語"), testTableCell("200")},
		},
		Alignments: []tui.Alignment{tui.AlignLeft, tui.AlignRight},
	}

	lines := table.Render(24, 10)
	joined := strings.Join(lineTexts(lines), "\n")
	require.Contains(t, joined, "Name")
	require.Contains(t, joined, "Count")
	require.Contains(t, joined, testAlpha)
	require.Contains(t, joined, "語")

	for _, line := range lines {
		require.LessOrEqual(t, line.Width(), 24)
	}

	centeredTable := &tui.Table{
		Style:       tcell.StyleDefault,
		HeaderStyle: tcell.StyleDefault,
		BorderStyle: tcell.StyleDefault,
		Headers:     nil,
		Rows:        [][]tui.TableCell{{testTableCell("x")}},
		Alignments:  []tui.Alignment{tui.AlignCenter},
	}
	centered := centeredTable.Render(7, 10)
	require.Contains(t, strings.Join(lineTexts(centered), "\n"), " x ")

	buffer := tui.NewCellBuffer(24, 6, tcell.StyleDefault)
	table.Draw(buffer, testRect(0, 0, 24, 6))
	require.Equal(t, '╭', buffer.Cell(0, 0).Rune)
}
