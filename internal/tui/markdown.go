package tui

import (
	"fmt"
	"strings"
	"sync"

	"github.com/gdamore/tcell/v3"
	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/extension"
	extast "github.com/yuin/goldmark/v2/extension/ast"
	"github.com/yuin/goldmark/v2/parser"
)

const (
	markdownIndent = " "
	markdownBullet = "• "

	markdownQuote          = "┃ "
	markdownRule           = "─"
	markdownTableMaxHeight = 10_000
)

// MarkdownEngine lazily initializes and caches a goldmark parser.
// The zero value is ready to use. The parser is stateless and safe for
// concurrent use — each Parse call creates its own reader and AST context.
type MarkdownEngine struct {
	parser parser.Parser
	once   sync.Once
}

// render parses source and invokes the visitor for each top-level AST child.
// The parser is initialized on first call and reused for all subsequent calls.
func (engine *MarkdownEngine) render(source []byte, visitor func(ast.Node)) {
	engine.once.Do(func() {
		engine.parser = parser.New(parser.WithExtensions(
			extension.NewTableParser(),
			extension.NewStrikethroughParser(),
			extension.NewTaskListItemParser(),
		))
	})

	document := engine.parser.Parse(source)
	for child := document.FirstChild(); child != nil; child = child.NextSibling() {
		visitor(child)
	}
}

// MarkdownStyles configures MarkdownView rendering.
type MarkdownStyles struct {
	Text      tcell.Style
	Accent    tcell.Style
	Muted     tcell.Style
	Code      tcell.Style
	CodeTheme CodeTheme
}

// MarkdownView renders markdown into terminal lines.
// When Engine is nil, Render reuses the view-local parser across calls.
type MarkdownView struct {
	Engine *MarkdownEngine
	Lexer  *LexerEngine
	Text   string
	Styles MarkdownStyles
}

// MarkdownListItem identifies the rendered lines directly owned by a list item.
type MarkdownListItem struct {
	StartLine int
	EndLine   int
}

// MarkdownRender contains rendered lines and list item metadata.
type MarkdownRender struct {
	Lines     []Line
	ListItems []MarkdownListItem
}

// Render parses and renders markdown.
func (view *MarkdownView) Render(width, height int) []Line {
	return view.RenderDetailed(width, height).Lines
}

// RenderDetailed parses markdown and preserves rendered list item ranges.
func (view *MarkdownView) RenderDetailed(width, height int) MarkdownRender {
	if view == nil || width <= 0 || height <= 0 {
		return MarkdownRender{Lines: []Line{}, ListItems: []MarkdownListItem{}}
	}

	renderer := markdownRenderer{
		styles:    view.Styles,
		source:    []byte(view.Text),
		lines:     []Line{},
		listItems: []MarkdownListItem{},
		width:     max(1, width),
		lexer:     view.Lexer,
	}

	engine := view.engine()
	engine.render(renderer.source, func(child ast.Node) {
		renderer.renderNode(child, markdownIndent)
	})

	firstLine := max(0, len(renderer.lines)-height)

	items := make([]MarkdownListItem, 0, len(renderer.listItems))
	for _, item := range renderer.listItems {
		if item.EndLine <= firstLine {
			continue
		}

		items = append(items, MarkdownListItem{
			StartLine: max(0, item.StartLine-firstLine),
			EndLine:   item.EndLine - firstLine,
		})
	}

	return MarkdownRender{Lines: Tail(renderer.lines, height), ListItems: items}
}

// engine returns the injected parser engine or a new lazily initialized engine.
func (view *MarkdownView) engine() *MarkdownEngine {
	if view.Engine != nil {
		return view.Engine
	}

	view.Engine = &MarkdownEngine{parser: nil, once: sync.Once{}}

	return view.Engine
}

// Draw draws markdown into rect.
func (view *MarkdownView) Draw(screen ContentSetter, rect Rect) {
	DrawLines(screen, rect, view.Render(rect.Width, rect.Height))
}

type markdownRenderer struct {
	lexer     *LexerEngine
	source    []byte
	lines     []Line
	listItems []MarkdownListItem
	styles    MarkdownStyles
	width     int
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
		renderer.appendWrappedLines(renderer.inlineText(typedNode), indent, renderer.styles.Text)
	case *ast.CodeBlock:
		renderer.renderCodeBlock(typedNode, indent)
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
	rule := strings.Repeat(markdownRule, max(1, renderer.width-Width(indent)))
	renderer.lines = append(renderer.lines, NewLine(renderer.styles.Muted, indent+rule))
}

func (renderer *markdownRenderer) renderHeading(node *ast.Heading, indent string) {
	text := renderer.inlineText(node)
	prefix := strings.Repeat("#", node.Level) + " "
	renderer.appendWrappedLines(prefix+text, indent, renderer.styles.Accent.Bold(true))
}

func (renderer *markdownRenderer) renderCodeBlock(node *ast.CodeBlock, indent string) {
	language, _ := node.Language(renderer.source)
	renderer.appendCodeLines(language, renderer.codeBlockText(node), indent)
}

func (renderer *markdownRenderer) appendCodeLines(language, text, indent string) {
	width := max(1, renderer.width-Width(indent))

	rendered := newLexerCodeRenderer(renderer.lexer).
		render(language, text, renderer.styles.CodeTheme, renderer.styles.Code)

	for _, line := range WrapCodeLines(rendered, width) {
		renderer.prependIndentToLine(&line, indent, line.Style)
		renderer.lines = append(renderer.lines, line)
	}
}

func (renderer *markdownRenderer) prependIndentToLine(line *Line, indent string, indentStyle tcell.Style) {
	*line = line.WithPrefix(indent, indentStyle)
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
	continuationIndent := indent + strings.Repeat(markdownIndent, Width(marker))
	firstBlock := true
	itemRecorded := false

	for child := item.FirstChild(); child != nil; child = child.NextSibling() {
		blockIndent := continuationIndent
		if firstBlock {
			blockIndent = firstIndent
		}

		startLine := len(renderer.lines)

		switch typedChild := child.(type) {
		case *ast.Paragraph:
			renderer.appendListItemText(typedChild, blockIndent, continuationIndent)
		default:
			renderer.renderNode(typedChild, blockIndent)
		}

		if !itemRecorded && len(renderer.lines) > startLine {
			if _, ok := child.(*ast.Paragraph); ok {
				renderer.listItems = append(renderer.listItems, MarkdownListItem{
					StartLine: startLine,
					EndLine:   len(renderer.lines),
				})
				itemRecorded = true
			}
		}

		firstBlock = false
	}
}

func (renderer *markdownRenderer) appendListItemText(node ast.Node, blockIndent, continuationIndent string) {
	renderer.appendWrappedLinesWithContinuation(
		renderer.inlineText(node),
		blockIndent,
		continuationIndent,
		renderer.styles.Text,
	)
}

func (renderer *markdownRenderer) appendWrappedLines(text, indent string, style tcell.Style) {
	renderer.appendWrappedLinesWithContinuation(text, indent, indent, style)
}

func (renderer *markdownRenderer) appendWrappedLinesWithContinuation(
	text string,
	firstIndent string,
	continuationIndent string,
	style tcell.Style,
) {
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
			builder.WriteString(typedChild.Value.Value(renderer.source))

			if typedChild.SoftLineBreak() || typedChild.HardLineBreak() {
				builder.WriteString(" ")
			}
		case *ast.CodeSpan:
			builder.WriteString("`")
			builder.WriteString(typedChild.Value.Value(renderer.source))
			builder.WriteString("`")
		case *ast.Link:
			label := renderer.inlineText(typedChild)
			builder.WriteString(label)

			destination := typedChild.Destination.Value(renderer.source)
			if destination != "" {
				builder.WriteString(" (")
				builder.WriteString(destination)
				builder.WriteString(")")
			}
		default:
			builder.WriteString(renderer.inlineText(typedChild))
		}
	}

	return strings.TrimSpace(builder.String())
}

func (renderer *markdownRenderer) codeBlockText(node *ast.CodeBlock) string {
	return string(node.Value.Bytes(renderer.source))
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
		body, ok := child.(*extast.TableBody)
		if !ok {
			continue
		}

		for rowNode := body.FirstChild(); rowNode != nil; rowNode = rowNode.NextSibling() {
			row, ok := rowNode.(*extast.TableRow)
			if !ok {
				continue
			}

			rows = append(rows, adapter.cells(row, adapter.renderer.styles.Text))
		}
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
	case extast.AlignLeft, extast.AlignNone:
		return AlignLeft
	case extast.AlignRight:
		return AlignRight
	case extast.AlignCenter:
		return AlignCenter
	default:
		return AlignLeft
	}
}
