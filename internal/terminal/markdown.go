package terminal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/omarluq/librecode/internal/terminal/rendertext"
	"strings"

	"github.com/gdamore/tcell/v3"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	goldtext "github.com/yuin/goldmark/text"
)

const (
	markdownIndent     = " "
	markdownBullet     = "• "
	markdownQuote      = "┃ "
	markdownRule       = "─"
	markdownCodePrefix = "  "
)

var terminalMarkdown = goldmark.New(goldmark.WithExtensions(extension.GFM))

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
	document := terminalMarkdown.Parser().Parse(goldtext.NewReader(renderer.source))
	renderer.renderChildren(document, markdownIndent)
	if len(renderer.lines) == 0 {
		return []rendertext.Line{rendertext.NewLine(app.theme.style(colorText), markdownIndent)}
	}

	return renderer.lines
}

func (renderer *markdownRenderer) renderChildren(parent ast.Node, indent string) {
	for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
		renderer.renderBlock(child, indent)
	}
}

func (renderer *markdownRenderer) renderBlock(node ast.Node, indent string) {
	switch typed := node.(type) {
	case *ast.Paragraph:
		renderer.renderParagraph(typed, indent, renderer.theme.style(colorText))
	case *ast.Heading:
		renderer.renderHeading(typed, indent)
	case *ast.FencedCodeBlock:
		renderer.renderCodeBlock(typed.Lines(), string(typed.Language(renderer.source)))
	case *ast.CodeBlock:
		renderer.renderCodeBlock(typed.Lines(), "")
	case *ast.Blockquote:
		renderer.renderChildren(typed, indent+markdownQuote)
	case *ast.List:
		renderer.renderList(typed, indent)
	case *ast.ThematicBreak:
		ruleWidth := max(3, renderer.width-rendertext.RuneLen(indent))
		renderer.appendLine(indent+strings.Repeat(markdownRule, ruleWidth), colorDim)
	case *extast.Table:
		renderer.renderTable(typed, indent)
	default:
		renderer.renderFallback(node, indent)
	}
}

func (renderer *markdownRenderer) renderParagraph(node ast.Node, indent string, style tcell.Style) {
	renderer.renderParagraphWithContinuation(node, indent, indent, style)
}

func (renderer *markdownRenderer) renderParagraphWithContinuation(
	node ast.Node,
	firstIndent string,
	continuationIndent string,
	style tcell.Style,
) {
	text := strings.TrimSpace(renderer.inlineText(node))
	if text == "" {
		return
	}
	renderer.appendWrappedWithContinuation(firstIndent, continuationIndent, text, style)
}

func (renderer *markdownRenderer) renderHeading(node *ast.Heading, indent string) {
	renderer.renderHeadingWithContinuation(node, indent, indent)
}

func (renderer *markdownRenderer) renderHeadingWithContinuation(
	node *ast.Heading,
	firstIndent string,
	continuationIndent string,
) {
	text := strings.TrimSpace(renderer.inlineText(node))
	if text == "" {
		return
	}
	prefix := strings.Repeat("#", min(max(1, node.Level), 6)) + " "
	renderer.appendWrappedWithContinuation(
		firstIndent,
		continuationIndent,
		prefix+text,
		renderer.theme.style(colorAccent).Bold(true),
	)
}

func (renderer *markdownRenderer) renderCodeBlock(segments *goldtext.Segments, language string) {
	text := strings.TrimRight(string(segments.Value(renderer.source)), "\n")
	if strings.EqualFold(language, "diff") || looksLikeDiff(text) {
		renderer.renderDiff(text)
		return
	}
	style := renderer.theme.background(colorCodeBg).Foreground(renderer.theme.colors[colorCodeText])
	renderer.appendCodeBlockLines(syntaxHighlightedCodeLines(language, text, *renderer.theme, style))
}

func (renderer *markdownRenderer) renderDiff(text string) {
	baseStyle := renderer.theme.background(colorCodeBg).Foreground(renderer.theme.colors[colorCodeText])
	renderer.appendCodeBlockLines(diffStyledLines(text, *renderer.theme, baseStyle))
}

func (renderer *markdownRenderer) renderList(list *ast.List, indent string) {
	ordinal := list.Start
	for item := list.FirstChild(); item != nil; item = item.NextSibling() {
		marker := markdownBullet
		if list.IsOrdered() {
			marker = fmt.Sprintf("%d. ", ordinal)
			ordinal++
		}
		renderer.renderListItem(item, indent, marker)
	}
}

func (renderer *markdownRenderer) renderListItem(item ast.Node, indent, marker string) {
	first := true
	childIndent := indent + strings.Repeat(" ", rendertext.RuneLen(marker))
	for child := item.FirstChild(); child != nil; child = child.NextSibling() {
		currentIndent := childIndent
		if first {
			currentIndent = indent + marker
			first = false
		}
		if renderer.renderListItemFirstBlock(child, currentIndent, childIndent) {
			continue
		}
		renderer.renderBlock(child, currentIndent)
	}
}

func (renderer *markdownRenderer) renderListItemFirstBlock(
	child ast.Node,
	firstIndent string,
	continuationIndent string,
) bool {
	if firstIndent == continuationIndent {
		return false
	}
	switch typed := child.(type) {
	case *ast.Paragraph:
		renderer.renderParagraphWithContinuation(
			typed,
			firstIndent,
			continuationIndent,
			renderer.theme.style(colorText),
		)
		return true
	case *ast.TextBlock:
		renderer.renderParagraphWithContinuation(
			typed,
			firstIndent,
			continuationIndent,
			renderer.theme.style(colorText),
		)
		return true
	case *ast.Heading:
		renderer.renderHeadingWithContinuation(typed, firstIndent, continuationIndent)
		return true
	default:
		return false
	}
}

func (renderer *markdownRenderer) renderFallback(node ast.Node, indent string) {
	if node.Type() == ast.TypeBlock && node.Lines() != nil {
		text := strings.TrimSpace(string(node.Lines().Value(renderer.source)))
		if text != "" {
			renderer.appendWrapped(indent, text, renderer.theme.style(colorText))
			return
		}
	}
	renderer.renderChildren(node, indent)
}

func (renderer *markdownRenderer) inlineText(node ast.Node) string {
	var builder strings.Builder
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		renderer.writeInlineText(&builder, child)
	}

	return builder.String()
}

func (renderer *markdownRenderer) writeInlineText(builder *strings.Builder, node ast.Node) {
	switch typed := node.(type) {
	case *ast.Text:
		builder.Write(typed.Segment.Value(renderer.source))
		if typed.HardLineBreak() || typed.SoftLineBreak() {
			builder.WriteByte('\n')
		}
	case *ast.String:
		builder.Write(typed.Value)
	case *ast.CodeSpan:
		builder.WriteString("`")
		builder.WriteString(strings.TrimSpace(renderer.inlineText(typed)))
		builder.WriteString("`")
	case *ast.Link:
		renderer.writeInlineChildren(builder, typed)
		if len(typed.Destination) > 0 {
			builder.WriteString(" (")
			builder.Write(typed.Destination)
			builder.WriteString(")")
		}
	default:
		renderer.writeInlineChildren(builder, node)
	}
}

func (renderer *markdownRenderer) writeInlineChildren(builder *strings.Builder, node ast.Node) {
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		renderer.writeInlineText(builder, child)
	}
}

func (renderer *markdownRenderer) appendWrapped(indent, text string, style tcell.Style) {
	renderer.appendWrappedWithContinuation(indent, indent, text, style)
}

func (renderer *markdownRenderer) appendWrappedWithContinuation(
	firstIndent string,
	continuationIndent string,
	text string,
	style tcell.Style,
) {
	availableWidth := max(1, renderer.width-rendertext.RuneLen(firstIndent))
	for index, line := range rendertext.Wrap(text, availableWidth) {
		lineIndent := firstIndent
		if index > 0 {
			lineIndent = continuationIndent
		}
		renderer.lines = append(renderer.lines, rendertext.NewLine(style, lineIndent+line))
	}
}

func (renderer *markdownRenderer) appendLine(text string, token colorToken) {
	renderer.lines = append(renderer.lines, rendertext.NewLine(renderer.theme.style(token), text))
}

func (renderer *markdownRenderer) appendCodeBlockLines(content []rendertext.Line) {
	innerWidth := max(1, renderer.width-rendertext.Width(markdownCodePrefix))
	for _, line := range content {
		renderer.lines = append(renderer.lines, wrapStyledLinePreserveWhitespace(line, innerWidth)...)
	}
}

func wrapStyledLinePreserveWhitespace(line rendertext.Line, width int) []rendertext.Line {
	if len(line.Spans) == 0 {
		return plainStyledWrappedLines(line, width)
	}
	return spanStyledWrappedLines(line, width)
}

func plainStyledWrappedLines(line rendertext.Line, width int) []rendertext.Line {
	wrapped := rendertext.WrapPreserveWhitespace(line.Text, width)
	lines := make([]rendertext.Line, 0, len(wrapped))
	for _, text := range wrapped {
		lines = append(lines, rendertext.NewLine(line.Style, text))
	}

	return lines
}

func spanStyledWrappedLines(line rendertext.Line, width int) []rendertext.Line {
	lines := []rendertext.Line{rendertext.NewLine(line.Style, "")}
	used := 0
	for _, span := range line.Spans {
		for _, segment := range rendertext.Segments(span.Text) {
			if used > 0 && used+segment.Width > width {
				lines = append(lines, rendertext.NewLine(line.Style, ""))
				used = 0
			}
			appendSegmentSpanToLastLine(&lines, segment.Text, span.Style)
			used += segment.Width
		}
	}
	if len(lines) == 0 {
		return []rendertext.Line{rendertext.NewLine(line.Style, "")}
	}

	return lines
}

func appendSegmentSpanToLastLine(lines *[]rendertext.Line, text string, style tcell.Style) {
	index := len(*lines) - 1
	line := (*lines)[index]
	line.Text += text
	line.Spans = append(line.Spans, rendertext.Span{Style: style, Text: text})
	(*lines)[index] = line
}

func codeStyledLines(text string, style tcell.Style) []rendertext.Line {
	lines := strings.Split(text, "\n")
	styled := make([]rendertext.Line, 0, len(lines))
	for _, line := range lines {
		styled = append(styled, rendertext.NewLine(style, markdownCodePrefix+line))
	}

	return styled
}

func diffStyledLines(text string, theme terminalTheme, baseStyle tcell.Style) []rendertext.Line {
	lines := strings.Split(text, "\n")
	styled := make([]rendertext.Line, 0, len(lines))
	for _, line := range lines {
		style := baseStyle
		switch {
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			style = baseStyle.Foreground(theme.colors[colorDiffAdd])
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			style = baseStyle.Foreground(theme.colors[colorDiffDelete])
		case strings.HasPrefix(line, "@@"):
			style = baseStyle.Foreground(theme.colors[colorAccent]).Bold(true)
		}
		styled = append(styled, rendertext.NewLine(style, line))
	}

	return styled
}

func looksLikeDiff(text string) bool {
	return strings.Contains(text, "\n+") && strings.Contains(text, "\n-")
}

func compactJSON(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	var buffer bytes.Buffer
	if err := json.Indent(&buffer, []byte(trimmed), "", "  "); err != nil {
		return trimmed
	}

	return buffer.String()
}
