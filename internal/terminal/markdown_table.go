package terminal

import (
	"strings"

	"github.com/gdamore/tcell/v3"
	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
)

const (
	markdownTableHorizontal  = "─"
	markdownTableVertical    = "│"
	markdownTableTopLeft     = "╭"
	markdownTableTopJoin     = "┬"
	markdownTableTopRight    = "╮"
	markdownTableMidLeft     = "├"
	markdownTableMidJoin     = "┼"
	markdownTableMidRight    = "┤"
	markdownTableBottomLeft  = "╰"
	markdownTableBottomJoin  = "┴"
	markdownTableBottomRight = "╯"
)

type markdownTableCell struct {
	text      string
	alignment extast.Alignment
	header    bool
}

type markdownTableRow struct {
	cells  []markdownTableCell
	header bool
}

func (renderer *markdownRenderer) renderTable(table *extast.Table, indent string) {
	rows := renderer.markdownTableRows(table)
	if len(rows) == 0 {
		return
	}
	columnWidths := markdownTableColumnWidths(rows)
	if len(columnWidths) == 0 {
		return
	}

	borderStyle := renderer.theme.style(colorBorderMuted)
	renderer.lines = append(renderer.lines, newStyledLine(borderStyle, indent+markdownTableBorderLine(
		markdownTableTopLeft,
		markdownTableTopJoin,
		markdownTableTopRight,
		columnWidths,
	)))
	for index, row := range rows {
		renderer.lines = append(renderer.lines, renderer.markdownTableStyledRow(indent, row, columnWidths))
		if row.header && index < len(rows)-1 {
			renderer.lines = append(renderer.lines, newStyledLine(borderStyle, indent+markdownTableBorderLine(
				markdownTableMidLeft,
				markdownTableMidJoin,
				markdownTableMidRight,
				columnWidths,
			)))
		}
	}
	renderer.lines = append(renderer.lines, newStyledLine(borderStyle, indent+markdownTableBorderLine(
		markdownTableBottomLeft,
		markdownTableBottomJoin,
		markdownTableBottomRight,
		columnWidths,
	)))
}

func (renderer *markdownRenderer) markdownTableRows(table *extast.Table) []markdownTableRow {
	rows := []markdownTableRow{}
	for child := table.FirstChild(); child != nil; child = child.NextSibling() {
		switch typed := child.(type) {
		case *extast.TableHeader:
			rows = append(rows, renderer.markdownTableRow(typed, true))
		case *extast.TableRow:
			rows = append(rows, renderer.markdownTableRow(typed, false))
		}
	}

	return rows
}

func (renderer *markdownRenderer) markdownTableRow(row ast.Node, header bool) markdownTableRow {
	cells := []markdownTableCell{}
	for child := row.FirstChild(); child != nil; child = child.NextSibling() {
		cell, ok := child.(*extast.TableCell)
		if !ok {
			continue
		}
		cells = append(cells, markdownTableCell{
			text:      strings.TrimSpace(renderer.inlineText(cell)),
			alignment: cell.Alignment,
			header:    header,
		})
	}

	return markdownTableRow{cells: cells, header: header}
}

func markdownTableColumnWidths(rows []markdownTableRow) []int {
	columnCount := 0
	for _, row := range rows {
		columnCount = max(columnCount, len(row.cells))
	}
	widths := make([]int, columnCount)
	for _, row := range rows {
		for index, cell := range row.cells {
			widths[index] = max(widths[index], terminalTextWidth(cell.text))
		}
	}
	for index := range widths {
		widths[index] = max(1, widths[index])
	}

	return widths
}

func markdownTableBorderLine(left, join, right string, widths []int) string {
	parts := make([]string, 0, len(widths))
	for _, width := range widths {
		parts = append(parts, strings.Repeat(markdownTableHorizontal, width+2))
	}

	return left + strings.Join(parts, join) + right
}

func (renderer *markdownRenderer) markdownTableStyledRow(
	indent string,
	row markdownTableRow,
	widths []int,
) styledLine {
	borderStyle := renderer.theme.style(colorBorderMuted)
	line := newStyledLine(borderStyle, "")
	if indent != "" {
		appendMarkdownTableSpan(&line, indent, borderStyle)
	}
	appendMarkdownTableSpan(&line, markdownTableVertical, borderStyle)
	for index, width := range widths {
		cell := emptyMarkdownTableCell()
		if index < len(row.cells) {
			cell = row.cells[index]
		}
		style := renderer.markdownTableCellStyle(cell)
		appendMarkdownTableSpan(&line, " ", style)
		appendMarkdownTableSpan(&line, markdownTableAlignedText(cell.text, width, cell.alignment), style)
		appendMarkdownTableSpan(&line, " ", style)
		appendMarkdownTableSpan(&line, markdownTableVertical, borderStyle)
	}

	return line
}

func (renderer *markdownRenderer) markdownTableCellStyle(cell markdownTableCell) tcell.Style {
	style := renderer.theme.style(colorText)
	if cell.header {
		style = renderer.theme.style(colorAccent).Bold(true)
	}

	return style
}

func appendMarkdownTableSpan(line *styledLine, text string, style tcell.Style) {
	line.Text += text
	line.Spans = append(line.Spans, styledSpan{Style: style, Text: text})
}

func emptyMarkdownTableCell() markdownTableCell {
	return markdownTableCell{
		text:      "",
		alignment: extast.AlignNone,
		header:    false,
	}
}

func markdownTableAlignedText(text string, width int, alignment extast.Alignment) string {
	text = terminalTextFit(text, width)
	padding := max(0, width-terminalTextWidth(text))
	switch alignment {
	case extast.AlignRight:
		return strings.Repeat(" ", padding) + text
	case extast.AlignCenter:
		left := padding / 2
		return strings.Repeat(" ", left) + text + strings.Repeat(" ", padding-left)
	case extast.AlignLeft, extast.AlignNone:
		return text + strings.Repeat(" ", padding)
	}

	return text + strings.Repeat(" ", padding)
}
