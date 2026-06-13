package tui

import (
	"strings"

	"github.com/gdamore/tcell/v3"
)

// Alignment controls table cell alignment.
type Alignment int

const (
	AlignLeft Alignment = iota
	AlignCenter
	AlignRight
)

// TableCell is one table cell.
type TableCell struct {
	Text  string
	Style tcell.Style
}

// Table is a simple bordered table component.
type Table struct {
	Headers     []TableCell
	Rows        [][]TableCell
	Alignments  []Alignment
	Style       tcell.Style
	HeaderStyle tcell.Style
	BorderStyle tcell.Style
}

// Render returns table lines clipped to width and height.
func (table *Table) Render(width, height int) []Line {
	if table == nil || width <= 0 || height <= 0 {
		return []Line{}
	}

	rows := table.allRows()
	if len(rows) == 0 {
		return []Line{}
	}

	colCount := table.columnCount(rows)
	colWidths := table.columnWidths(rows, colCount, width)
	lines := []Line{NewLine(table.BorderStyle, table.tableBorder("╭", "┬", "╮", colWidths))}
	if len(table.Headers) > 0 {
		lines = append(lines, table.renderRow(table.Headers, colWidths, table.HeaderStyle))
		lines = append(lines, NewLine(table.BorderStyle, table.tableBorder("├", "┼", "┤", colWidths)))
	}
	for _, row := range table.Rows {
		lines = append(lines, table.renderRow(row, colWidths, table.Style))
	}
	lines = append(lines, NewLine(table.BorderStyle, table.tableBorder("╰", "┴", "╯", colWidths)))

	return Tail(lines, height)
}

// Draw draws table into rect.
func (table *Table) Draw(screen ContentSetter, rect Rect) {
	DrawLines(screen, rect, table.Render(rect.Width, rect.Height))
}

func (table *Table) allRows() [][]TableCell {
	rows := [][]TableCell{}
	if len(table.Headers) > 0 {
		rows = append(rows, table.Headers)
	}
	rows = append(rows, table.Rows...)

	return rows
}

func (table *Table) columnCount(rows [][]TableCell) int {
	count := 0
	for _, row := range rows {
		count = max(count, len(row))
	}

	return count
}

func (table *Table) columnWidths(rows [][]TableCell, colCount, maxWidth int) []int {
	if colCount == 0 {
		return []int{}
	}

	widths := make([]int, colCount)
	for _, row := range rows {
		for col, cell := range row {
			widths[col] = max(widths[col], Width(cell.Text))
		}
	}

	available := max(1, maxWidth-colCount-1-(colCount*2))
	for sumInts(widths) > available {
		largest := 0
		for index := range widths {
			if widths[index] > widths[largest] {
				largest = index
			}
		}
		if widths[largest] <= 1 {
			break
		}
		widths[largest]--
	}

	return widths
}

func (table *Table) renderRow(row []TableCell, widths []int, fallback tcell.Style) Line {
	spans := []Span{{Text: "│", Style: table.BorderStyle}}
	text := "│"
	for col, width := range widths {
		cell := TableCell{}
		if col < len(row) {
			cell = row[col]
		}
		style := cell.Style
		if style == (tcell.Style{}) {
			style = fallback
		}
		value := table.align(cell.Text, width, col)
		segment := " " + value + " "
		spans = append(spans, Span{Text: segment, Style: style}, Span{Text: "│", Style: table.BorderStyle})
		text += segment + "│"
	}

	return Line{Text: text, Style: fallback, Spans: spans}
}

func (table *Table) align(text string, width, column int) string {
	text = Truncate(text, width)
	padding := width - Width(text)
	alignment := AlignLeft
	if column < len(table.Alignments) {
		alignment = table.Alignments[column]
	}

	switch alignment {
	case AlignRight:
		return strings.Repeat(" ", padding) + text
	case AlignCenter:
		left := padding / 2
		return strings.Repeat(" ", left) + text + strings.Repeat(" ", padding-left)
	default:
		return text + strings.Repeat(" ", padding)
	}
}

func (table *Table) tableBorder(left, middle, right string, widths []int) string {
	parts := make([]string, 0, len(widths))
	for _, width := range widths {
		parts = append(parts, strings.Repeat("─", width+2))
	}

	return left + strings.Join(parts, middle) + right
}

func sumInts(values []int) int {
	total := 0
	for _, value := range values {
		total += value
	}

	return total
}
