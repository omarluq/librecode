package terminal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v3"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
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
	lines  []styledLine
	width  int
}

func (app *App) renderMarkdown(content string, width int) []styledLine {
	renderer := markdownRenderer{
		theme:  &app.theme,
		source: []byte(content),
		lines:  []styledLine{},
		width:  max(1, width),
	}
	document := terminalMarkdown.Parser().Parse(goldtext.NewReader(renderer.source))
	renderer.renderChildren(document, markdownIndent)
	if len(renderer.lines) == 0 {
		return []styledLine{newStyledLine(app.theme.style(colorText), markdownIndent)}
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
		renderer.appendLine(indent+strings.Repeat(markdownRule, max(3, renderer.width-runeLen(indent))), colorDim)
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
	childIndent := indent + strings.Repeat(" ", runeLen(marker))
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
	availableWidth := max(1, renderer.width-runeLen(firstIndent))
	for index, line := range wrapText(text, availableWidth) {
		lineIndent := firstIndent
		if index > 0 {
			lineIndent = continuationIndent
		}
		renderer.lines = append(renderer.lines, newStyledLine(style, lineIndent+line))
	}
}

func (renderer *markdownRenderer) appendLine(text string, token colorToken) {
	renderer.lines = append(renderer.lines, newStyledLine(renderer.theme.style(token), text))
}

func (renderer *markdownRenderer) appendCodeBlockLines(content []styledLine) {
	innerWidth := max(1, renderer.width-terminalTextWidth(markdownCodePrefix))
	for _, line := range content {
		renderer.lines = append(renderer.lines, wrapStyledLinePreserveWhitespace(line, innerWidth)...)
	}
}

func wrapStyledLinePreserveWhitespace(line styledLine, width int) []styledLine {
	if len(line.Spans) == 0 {
		return plainStyledWrappedLines(line, width)
	}
	return spanStyledWrappedLines(line, width)
}

func plainStyledWrappedLines(line styledLine, width int) []styledLine {
	wrapped := wrapTextPreserveWhitespace(line.Text, width)
	lines := make([]styledLine, 0, len(wrapped))
	for _, text := range wrapped {
		lines = append(lines, newStyledLine(line.Style, text))
	}

	return lines
}

func spanStyledWrappedLines(line styledLine, width int) []styledLine {
	lines := []styledLine{newStyledLine(line.Style, "")}
	used := 0
	for _, span := range line.Spans {
		for _, segment := range terminalTextSegments(span.Text) {
			if used > 0 && used+segment.Width > width {
				lines = append(lines, newStyledLine(line.Style, ""))
				used = 0
			}
			appendSegmentSpanToLastLine(&lines, segment.Text, span.Style)
			used += segment.Width
		}
	}
	if len(lines) == 0 {
		return []styledLine{newStyledLine(line.Style, "")}
	}

	return lines
}

func appendSegmentSpanToLastLine(lines *[]styledLine, text string, style tcell.Style) {
	index := len(*lines) - 1
	line := (*lines)[index]
	line.Text += text
	line.Spans = append(line.Spans, styledSpan{Style: style, Text: text})
	(*lines)[index] = line
}

func codeStyledLines(text string, style tcell.Style) []styledLine {
	lines := strings.Split(text, "\n")
	styled := make([]styledLine, 0, len(lines))
	for _, line := range lines {
		styled = append(styled, newStyledLine(style, markdownCodePrefix+line))
	}

	return styled
}

func diffStyledLines(text string, theme terminalTheme, baseStyle tcell.Style) []styledLine {
	lines := strings.Split(text, "\n")
	styled := make([]styledLine, 0, len(lines))
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
		styled = append(styled, newStyledLine(style, line))
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
