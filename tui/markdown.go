package tui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v3"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	goldtext "github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

const (
	markdownIndent                   = " "
	markdownBullet                   = "• "
	markdownQuote                    = "┃ "
	markdownRule                     = "─"
	markdownTableTransformerPriority = 200
	markdownStrikePriority           = 500
	markdownTableMaxHeight           = 10_000
)

// MarkdownStyles configures MarkdownView rendering.
type MarkdownStyles struct {
	Text      tcell.Style
	Accent    tcell.Style
	Muted     tcell.Style
	Code      tcell.Style
	CodeTheme CodeTheme
}

// MarkdownView renders markdown into terminal lines.
type MarkdownView struct {
	Text   string
	Styles MarkdownStyles
}

// Render parses and renders markdown.
func (view *MarkdownView) Render(width, height int) []Line {
	if view == nil || width <= 0 || height <= 0 {
		return []Line{}
	}

	renderer := markdownRenderer{
		styles: view.Styles,
		source: []byte(view.Text),
		lines:  []Line{},
		width:  max(1, width),
	}
	parser := newMarkdownParser()
	document := parser.Parser().Parse(goldtext.NewReader(renderer.source))
	renderer.renderChildren(document, markdownIndent)

	return Tail(renderer.lines, height)
}

// Draw draws markdown into rect.
func (view *MarkdownView) Draw(screen ContentSetter, rect Rect) {
	DrawLines(screen, rect, view.Render(rect.Width, rect.Height))
}

func newMarkdownParser() goldmark.Markdown {
	return goldmark.New(goldmark.WithParserOptions(
		parser.WithParagraphTransformers(util.Prioritized(
			extension.NewTableParagraphTransformer(),
			markdownTableTransformerPriority,
		)),
		parser.WithASTTransformers(util.Prioritized(extension.NewTableASTTransformer(), 0)),
		parser.WithInlineParsers(util.Prioritized(extension.NewStrikethroughParser(), markdownStrikePriority)),
		parser.WithInlineParsers(util.Prioritized(extension.NewTaskCheckBoxParser(), 0)),
	))
}

type markdownRenderer struct {
	styles MarkdownStyles
	source []byte
	lines  []Line
	width  int
}

func (renderer *markdownRenderer) renderChildren(parent ast.Node, indent string) {
	for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
		renderer.renderNode(child, indent)
	}
}

func (renderer *markdownRenderer) renderNode(node ast.Node, indent string) {
	switch typedNode := node.(type) {
	case *ast.Heading:
		renderer.renderHeading(typedNode, indent)
	case *ast.Paragraph:
		renderer.appendWrappedLines(renderer.inlineText(typedNode), indent, renderer.styles.Text.Bold(true))
	case *ast.FencedCodeBlock:
		renderer.renderCodeBlock(typedNode, indent)
	case *ast.CodeBlock:
		renderer.renderIndentedCodeBlock(typedNode, indent)
	case *ast.Blockquote:
		renderer.renderChildren(typedNode, indent+markdownQuote)
	case *ast.List:
		renderer.renderList(typedNode, indent)
	case *ast.ThematicBreak:
		renderer.renderThematicBreak(indent)
	case *extast.Table:
		renderer.renderTable(typedNode, indent)
	default:
		renderer.renderChildren(node, indent)
	}
}

func (renderer *markdownRenderer) renderThematicBreak(indent string) {
	rule := strings.Repeat(markdownRule, max(1, renderer.width-len(indent)))
	renderer.lines = append(renderer.lines, NewLine(renderer.styles.Muted, indent+rule))
}

func (renderer *markdownRenderer) renderHeading(node *ast.Heading, indent string) {
	text := renderer.inlineText(node)
	prefix := strings.Repeat("#", node.Level) + " "
	renderer.appendWrappedLines(prefix+text, indent, renderer.styles.Accent.Bold(true))
}

func (renderer *markdownRenderer) renderCodeBlock(node *ast.FencedCodeBlock, indent string) {
	language := ""
	if node.Language(renderer.source) != nil {
		language = string(node.Language(renderer.source))
	}
	renderer.appendCodeLines(language, renderer.codeBlockText(node), indent)
}

func (renderer *markdownRenderer) renderIndentedCodeBlock(node *ast.CodeBlock, indent string) {
	renderer.appendCodeLines("", renderer.codeBlockText(node), indent)
}

func (renderer *markdownRenderer) appendCodeLines(language, text, indent string) {
	for _, line := range SyntaxHighlightedCodeLines(language, text, renderer.styles.CodeTheme, renderer.styles.Code) {
		renderer.prependIndentToLine(&line, indent, line.Style)
		renderer.lines = append(renderer.lines, line)
	}
}

func (renderer *markdownRenderer) prependIndentToLine(line *Line, indent string, indentStyle tcell.Style) {
	line.Text = indent + line.Text
	if len(line.Spans) == 0 {
		line.Spans = []Span{{Text: line.Text, Style: line.Style}}
		return
	}

	line.Spans = append([]Span{{Text: indent, Style: indentStyle}}, line.Spans...)
}

func (renderer *markdownRenderer) renderList(node *ast.List, indent string) {
	index := 1
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		item, ok := child.(*ast.ListItem)
		if !ok {
			continue
		}

		marker := markdownBullet
		if node.IsOrdered() {
			marker = fmt.Sprintf("%d. ", index)
		}
		renderer.renderListItem(item, indent, marker)
		index++
	}
}

func (renderer *markdownRenderer) renderListItem(item *ast.ListItem, indent, marker string) {
	firstIndent := indent + marker
	continuationIndent := indent + strings.Repeat(markdownIndent, len(marker))
	firstBlock := true
	for child := item.FirstChild(); child != nil; child = child.NextSibling() {
		blockIndent := continuationIndent
		if firstBlock {
			blockIndent = firstIndent
		}
		switch typedChild := child.(type) {
		case *ast.Paragraph:
			renderer.appendWrappedLinesWithContinuation(renderer.inlineText(typedChild), blockIndent, continuationIndent, renderer.styles.Text.Bold(true))
		case *ast.TextBlock:
			renderer.appendWrappedLinesWithContinuation(renderer.inlineText(typedChild), blockIndent, continuationIndent, renderer.styles.Text.Bold(true))
		default:
			renderer.renderNode(typedChild, blockIndent)
		}
		firstBlock = false
	}
}

func (renderer *markdownRenderer) appendWrappedLines(text, indent string, style tcell.Style) {
	renderer.appendWrappedLinesWithContinuation(text, indent, indent, style)
}

func (renderer *markdownRenderer) appendWrappedLinesWithContinuation(text, firstIndent, continuationIndent string, style tcell.Style) {
	width := max(1, renderer.width-Width(firstIndent))
	wrapped := Wrap(text, width)
	for index, line := range wrapped {
		indent := firstIndent
		if index > 0 {
			indent = continuationIndent
		}
		renderer.lines = append(renderer.lines, NewLine(style, indent+line))
	}
}

func (renderer *markdownRenderer) inlineText(node ast.Node) string {
	var builder strings.Builder
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		switch typedChild := child.(type) {
		case *ast.Text:
			builder.Write(typedChild.Segment.Value(renderer.source))
			if typedChild.SoftLineBreak() || typedChild.HardLineBreak() {
				builder.WriteString(" ")
			}
		case *ast.CodeSpan:
			builder.WriteString("`")
			builder.WriteString(renderer.inlineText(typedChild))
			builder.WriteString("`")
		case *ast.String:
			builder.Write(typedChild.Value)
		case *ast.Link:
			label := renderer.inlineText(typedChild)
			builder.WriteString(label)
			if len(typedChild.Destination) > 0 {
				builder.WriteString(" (")
				builder.Write(typedChild.Destination)
				builder.WriteString(")")
			}
		default:
			builder.WriteString(renderer.inlineText(typedChild))
		}
	}

	return strings.TrimSpace(builder.String())
}

func (renderer *markdownRenderer) codeBlockText(node ast.Node) string {
	var builder strings.Builder
	lines := node.Lines()
	for index := 0; index < lines.Len(); index++ {
		segment := lines.At(index)
		builder.Write(segment.Value(renderer.source))
	}

	return builder.String()
}

func (renderer *markdownRenderer) renderTable(node *extast.Table, indent string) {
	adapter := markdownTableAdapter{renderer: renderer}
	table := &Table{
		Headers:     adapter.headers(node),
		Rows:        adapter.rows(node),
		Alignments:  adapter.alignments(node),
		Style:       renderer.styles.Text,
		HeaderStyle: renderer.styles.Accent.Bold(true),
		BorderStyle: renderer.styles.Muted,
	}

	for _, line := range table.Render(max(1, renderer.width-Width(indent)), markdownTableMaxHeight) {
		renderer.prependIndentToLine(&line, indent, renderer.styles.Muted)
		renderer.lines = append(renderer.lines, line)
	}
}

type markdownTableAdapter struct {
	renderer *markdownRenderer
}

func (adapter markdownTableAdapter) headers(node *extast.Table) []TableCell {
	header := adapter.headerNode(node)
	if header == nil {
		return nil
	}

	return adapter.cells(header, adapter.renderer.styles.Accent.Bold(true))
}

func (adapter markdownTableAdapter) rows(node *extast.Table) [][]TableCell {
	rows := [][]TableCell{}
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		row, ok := child.(*extast.TableRow)
		if !ok {
			continue
		}
		rows = append(rows, adapter.cells(row, adapter.renderer.styles.Text))
	}

	return rows
}

func (adapter markdownTableAdapter) alignments(node *extast.Table) []Alignment {
	header := adapter.headerNode(node)
	if header == nil {
		return nil
	}

	alignments := []Alignment{}
	for child := header.FirstChild(); child != nil; child = child.NextSibling() {
		cell, ok := child.(*extast.TableCell)
		if !ok {
			continue
		}
		alignments = append(alignments, markdownTableAlignment(cell.Alignment))
	}

	return alignments
}

func (adapter markdownTableAdapter) headerNode(node *extast.Table) *extast.TableHeader {
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		header, ok := child.(*extast.TableHeader)
		if ok {
			return header
		}
	}

	return nil
}

func (adapter markdownTableAdapter) cells(row ast.Node, style tcell.Style) []TableCell {
	cells := []TableCell{}
	for child := row.FirstChild(); child != nil; child = child.NextSibling() {
		cell, ok := child.(*extast.TableCell)
		if !ok {
			continue
		}
		cells = append(cells, TableCell{Text: strings.TrimSpace(adapter.renderer.inlineText(cell)), Style: style})
	}

	return cells
}

func markdownTableAlignment(alignment extast.Alignment) Alignment {
	switch alignment {
	case extast.AlignRight:
		return AlignRight
	case extast.AlignCenter:
		return AlignCenter
	default:
		return AlignLeft
	}
}
