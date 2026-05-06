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
		return []styledLine{{Style: app.theme.style(colorText), Text: markdownIndent}}
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
	text := strings.TrimSpace(renderer.inlineText(node))
	if text == "" {
		return
	}
	renderer.appendWrapped(indent, text, style)
}

func (renderer *markdownRenderer) renderHeading(node *ast.Heading, indent string) {
	text := strings.TrimSpace(renderer.inlineText(node))
	if text == "" {
		return
	}
	prefix := strings.Repeat("#", min(max(1, node.Level), 6)) + " "
	renderer.appendWrapped(indent, prefix+text, renderer.theme.style(colorAccent).Bold(true))
}

func (renderer *markdownRenderer) renderCodeBlock(segments *goldtext.Segments, language string) {
	text := strings.TrimRight(string(segments.Value(renderer.source)), "\n")
	if strings.EqualFold(language, "diff") || looksLikeDiff(text) {
		renderer.renderDiff(text)
		return
	}
	style := renderer.theme.background(colorCodeBg).Foreground(renderer.theme.colors[colorCodeText])
	label := strings.TrimSpace(language)
	if label == "" {
		label = "code"
	}
	renderer.appendCodeFrame(label, text, style)
}

func (renderer *markdownRenderer) renderDiff(text string) {
	baseStyle := renderer.theme.background(colorCodeBg).Foreground(renderer.theme.colors[colorCodeText])
	renderer.appendCodeFrameLines("diff", diffStyledLines(text, *renderer.theme, baseStyle))
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
		renderer.renderBlock(child, currentIndent)
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
	availableWidth := max(1, renderer.width-runeLen(indent))
	for _, line := range wrapText(text, availableWidth) {
		renderer.lines = append(renderer.lines, styledLine{Style: style, Text: indent + line})
	}
}

func (renderer *markdownRenderer) appendLine(text string, token colorToken) {
	renderer.lines = append(renderer.lines, styledLine{Style: renderer.theme.style(token), Text: text})
}

func (renderer *markdownRenderer) appendCodeFrame(label, text string, style tcell.Style) {
	renderer.appendCodeFrameLines(label, codeStyledLines(text, style))
}

func (renderer *markdownRenderer) appendCodeFrameLines(label string, content []styledLine) {
	width := renderer.width
	borderStyle := renderer.theme.background(colorCodeBg).Foreground(renderer.theme.colors[colorBorderMuted])
	renderer.lines = append(renderer.lines, styledLine{Style: borderStyle, Text: boxTop(width, label)})
	innerWidth := max(1, width-4)
	for _, line := range content {
		for _, wrapped := range wrapText(line.Text, innerWidth) {
			text := "│ " + padRight(wrapped, innerWidth) + " │"
			renderer.lines = append(renderer.lines, styledLine{Style: line.Style, Text: text})
		}
	}
	renderer.lines = append(renderer.lines, styledLine{Style: borderStyle, Text: boxBottom(width)})
}

func codeStyledLines(text string, style tcell.Style) []styledLine {
	lines := strings.Split(text, "\n")
	styled := make([]styledLine, 0, len(lines))
	for _, line := range lines {
		styled = append(styled, styledLine{Style: style, Text: markdownCodePrefix + line})
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
		styled = append(styled, styledLine{Style: style, Text: line})
	}

	return styled
}

func looksLikeDiff(text string) bool {
	return strings.Contains(text, "\n+") && strings.Contains(text, "\n-")
}

func boxTop(width int, label string) string {
	prefix := "╭─ " + label + " "
	return prefix + strings.Repeat("─", max(1, width-runeLen(prefix)-1)) + "╮"
}

func boxBottom(width int) string {
	return "╰" + strings.Repeat("─", max(1, width-2)) + "╯"
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
