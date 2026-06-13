package terminal

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

	"github.com/omarluq/librecode/internal/terminal/rendertext"
)

const (
	markdownIndent                   = " "
	markdownBullet                   = "• "
	markdownQuote                    = "┃ "
	markdownRule                     = "─"
	markdownCodePrefix               = "  "
	markdownTableTransformerPriority = 200
	markdownStrikePriority           = 500
)

type terminalMarkdown struct {
	parser goldmark.Markdown
}

func newTerminalMarkdown() terminalMarkdown {
	return terminalMarkdown{parser: goldmark.New(goldmark.WithParserOptions(
		parser.WithParagraphTransformers(util.Prioritized(
			extension.NewTableParagraphTransformer(),
			markdownTableTransformerPriority,
		)),
		parser.WithASTTransformers(util.Prioritized(extension.NewTableASTTransformer(), 0)),
		parser.WithInlineParsers(util.Prioritized(extension.NewStrikethroughParser(), markdownStrikePriority)),
		parser.WithInlineParsers(util.Prioritized(extension.NewTaskCheckBoxParser(), 0)),
	))}
}

func (markdown terminalMarkdown) ParseInto(source []byte, renderer *markdownRenderer) {
	document := markdown.parser.Parser().Parse(goldtext.NewReader(source))
	renderer.renderChildren(document, markdownIndent)
}

type markdownRenderer struct {
	theme  *terminalTheme
	source []byte
	lines  []rendertext.Line
	width  int
}

func (app *App) renderMarkdown(content string, width int) []rendertext.Line {
	renderer := markdownRenderer{
		theme:  &app.theme,
		source: []byte(content),
		lines:  []rendertext.Line{},
		width:  max(1, width),
	}
	newTerminalMarkdown().ParseInto(renderer.source, &renderer)

	return renderer.lines
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
		renderer.appendWrappedLines(renderer.inlineText(typedNode), indent, renderer.theme.style(colorText).Bold(true))
	case *ast.FencedCodeBlock:
		renderer.renderCodeBlock(typedNode, indent)
	case *ast.CodeBlock:
		renderer.renderIndentedCodeBlock(typedNode, indent)
	case *ast.Blockquote:
		renderer.renderBlockquote(typedNode, indent)
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
	renderer.lines = append(renderer.lines, rendertext.NewLine(renderer.theme.style(colorMuted), indent+rule))
}

func (renderer *markdownRenderer) renderHeading(node *ast.Heading, indent string) {
	text := renderer.inlineText(node)
	prefix := strings.Repeat("#", node.Level) + " "
	renderer.appendWrappedLines(prefix+text, indent, renderer.theme.style(colorAccent).Bold(true))
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
	baseStyle := renderer.theme.background(colorCodeBg).Foreground(renderer.theme.colors[colorCodeText])
	for _, line := range syntaxHighlightedCodeLines(language, text, *renderer.theme, baseStyle) {
		line.Text = indent + line.Text
		if len(line.Spans) == 0 {
			line.Spans = []rendertext.Span{{Text: line.Text, Style: line.Style}}
		} else {
			line.Spans = append([]rendertext.Span{{Text: indent, Style: line.Style}}, line.Spans...)
		}

		renderer.lines = append(renderer.lines, line)
	}
}

func (renderer *markdownRenderer) renderBlockquote(node *ast.Blockquote, indent string) {
	renderer.renderChildren(node, indent+markdownQuote)
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
			renderer.appendWrappedLinesWithContinuation(
				renderer.inlineText(typedChild),
				blockIndent,
				continuationIndent,
				renderer.theme.style(colorText).Bold(true),
			)
		case *ast.TextBlock:
			renderer.appendWrappedLinesWithContinuation(
				renderer.inlineText(typedChild),
				blockIndent,
				continuationIndent,
				renderer.theme.style(colorText).Bold(true),
			)
		case *ast.List:
			renderer.renderList(typedChild, continuationIndent)
		default:
			renderer.renderNode(typedChild, blockIndent)
		}

		firstBlock = false
	}
}

func (renderer *markdownRenderer) inlineText(parent ast.Node) string {
	var output strings.Builder
	renderer.writeInlineText(&output, parent)

	return strings.TrimSpace(output.String())
}

func (renderer *markdownRenderer) writeInlineText(output *strings.Builder, node ast.Node) {
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		switch typedNode := child.(type) {
		case *ast.Text:
			output.Write(typedNode.Segment.Value(renderer.source))

			if typedNode.SoftLineBreak() || typedNode.HardLineBreak() {
				output.WriteByte(' ')
			}
		case *ast.CodeSpan:
			output.WriteByte('`')
			renderer.writeInlineText(output, typedNode)
			output.WriteByte('`')
		case *ast.String:
			output.Write(typedNode.Value)
		case *ast.Link:
			renderer.writeInlineText(output, typedNode)

			if len(typedNode.Destination) > 0 {
				output.WriteString(" (")
				output.Write(typedNode.Destination)
				output.WriteByte(')')
			}
		default:
			renderer.writeInlineText(output, child)
		}
	}
}

func (renderer *markdownRenderer) codeBlockText(node ast.Node) string {
	var output strings.Builder

	lines := node.Lines()
	for lineIndex := range lines.Len() {
		segment := lines.At(lineIndex)
		output.Write(segment.Value(renderer.source))
	}

	return output.String()
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
	availableWidth := max(1, renderer.width-len(firstIndent))
	words := strings.Fields(text)
	current := firstIndent
	lineIndent := firstIndent

	for _, word := range words {
		var candidate string
		if strings.TrimSpace(strings.TrimPrefix(current, lineIndent)) == "" {
			candidate = lineIndent + word
		} else {
			candidate = current + " " + word
		}

		if rendertext.Width(candidate)-len(lineIndent) > availableWidth && strings.TrimSpace(current) != "" {
			renderer.lines = append(renderer.lines, rendertext.NewLine(style, current))
			lineIndent = continuationIndent
			availableWidth = max(1, renderer.width-len(lineIndent))
			current = lineIndent + word
		} else {
			current = candidate
		}
	}

	if strings.TrimSpace(current) != "" {
		renderer.lines = append(renderer.lines, rendertext.NewLine(style, current))
	}
}
