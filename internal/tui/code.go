package tui

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/gdamore/tcell/v3"
)

const minCodeBodyWidth = 12

// CodeTheme maps Chroma token groups to tcell colors.
type CodeTheme struct {
	Text    tcell.Color
	Accent  tcell.Color
	Success tcell.Color
	Warning tcell.Color
	Dim     tcell.Color
	Muted   tcell.Color
	DiffAdd tcell.Color
	DiffDel tcell.Color
}

// CodeBlock renders syntax-highlighted code.
type CodeBlock struct {
	Style    tcell.Style
	Engine   *LexerEngine
	Language string
	Text     string
	Theme    CodeTheme
}

// Render returns highlighted code lines.
func (block *CodeBlock) Render(width, height int) []Line {
	if block == nil || width <= 0 || height <= 0 {
		return []Line{}
	}

	rendered := newLexerCodeRenderer(block.Engine).render(block.Language, block.Text, block.Theme, block.Style)
	lines := WrapCodeLines(rendered, width)

	return Tail(lines, height)
}

// Draw draws highlighted code.
func (block *CodeBlock) Draw(screen ContentSetter, rect Rect) {
	DrawLines(screen, rect, block.Render(rect.Width, rect.Height))
}

// DiffView renders diff lines with add/delete coloring.
type DiffView struct {
	Style tcell.Style
	Text  string
	Theme CodeTheme
}

// Render returns diff lines.
func (view *DiffView) Render(width, height int) []Line {
	if view == nil || width <= 0 || height <= 0 {
		return []Line{}
	}

	lines := WrapCodeLines(DiffStyledLines(view.Text, view.Theme, view.Style), width)

	return Tail(lines, height)
}

// Draw draws diff lines.
func (view *DiffView) Draw(screen ContentSetter, rect Rect) {
	DrawLines(screen, rect, view.Render(rect.Width, rect.Height))
}

// SyntaxHighlightedCodeLines returns syntax-highlighted code lines.
func SyntaxHighlightedCodeLines(language, text string, theme CodeTheme, baseStyle tcell.Style) []Line {
	return newLexerCodeRenderer(nil).render(language, text, theme, baseStyle)
}

// lexerCodeRenderer owns a cache-backed lexer engine and renders
// syntax-highlighted code lines for code blocks.
type lexerCodeRenderer struct {
	engine *LexerEngine
}

func newLexerCodeRenderer(engine *LexerEngine) lexerCodeRenderer {
	return lexerCodeRenderer{engine: engine}
}

func (r lexerCodeRenderer) render(language, text string, theme CodeTheme, baseStyle tcell.Style) []Line {
	iterator, ok := r.iterator(language, text)
	if !ok {
		return codeStyledLines(text, baseStyle)
	}

	lines := codeLinesFromTokens(iterator.Tokens(), theme, baseStyle)
	if len(lines) == 0 {
		return []Line{NewLine(baseStyle, "  ")}
	}

	return lines
}

func (r lexerCodeRenderer) iterator(language, text string) (chroma.Iterator, bool) {
	lexer := lexers.Get(strings.TrimSpace(language))
	if lexer != nil {
		return tokenizeCode(lexer, text)
	}

	if strings.TrimSpace(language) != "" {
		return nil, false
	}

	if r.engine != nil {
		return r.engine.IteratorFor(text)
	}

	return analyzedCodeIterator(text)
}

func tokenizeCode(lexer chroma.Lexer, text string) (chroma.Iterator, bool) {
	iterator, err := chroma.Coalesce(lexer).Tokenise(nil, text)
	if err != nil {
		return nil, false
	}

	return iterator, true
}

func analyzedCodeIterator(text string) (chroma.Iterator, bool) {
	var bestLexer chroma.Lexer

	highest := float32(0)

	for _, lexer := range lexers.GlobalLexerRegistry.Lexers {
		weight := lexer.AnalyseText(text)
		if weight > highest {
			highest = weight
			bestLexer = lexer
		}
	}

	if highest > 0 && bestLexer != nil {
		return tokenizeCode(bestLexer, text)
	}

	return nil, false
}

func codeStyledLines(text string, baseStyle tcell.Style) []Line {
	parts := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(parts) == 0 {
		return []Line{NewLine(baseStyle, "")}
	}

	lines := make([]Line, 0, len(parts))
	for _, part := range parts {
		lines = append(lines, NewLine(baseStyle, part))
	}

	return lines
}

// DiffStyledLines returns styled diff lines.
func DiffStyledLines(text string, theme CodeTheme, baseStyle tcell.Style) []Line {
	parts := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(parts) == 0 {
		return nil
	}

	lines := make([]Line, 0, len(parts))
	for _, part := range parts {
		style := baseStyle
		if strings.HasPrefix(part, "+") {
			style = baseStyle.Foreground(theme.DiffAdd)
		} else if strings.HasPrefix(part, "-") {
			style = baseStyle.Foreground(theme.DiffDel)
		}

		lines = append(lines, NewLine(style, part))
	}

	return lines
}

func codeLinesFromTokens(tokens []chroma.Token, theme CodeTheme, baseStyle tcell.Style) []Line {
	lines := []Line{NewLine(baseStyle, "")}

	for _, token := range tokens {
		if token.Type == chroma.EOFType {
			break
		}

		segments := strings.SplitAfter(token.Value, "\n")
		for _, segment := range segments {
			if segment == "" {
				continue
			}

			if text, ok := strings.CutSuffix(segment, "\n"); ok {
				appendCodeSegment(&lines, text, token.Type, theme, baseStyle)
				lines = append(lines, NewLine(baseStyle, ""))

				continue
			}

			appendCodeSegment(&lines, segment, token.Type, theme, baseStyle)
		}
	}

	return trimTrailingEmptyCodeLine(lines, baseStyle)
}

func appendCodeSegment(
	lines *[]Line,
	segment string,
	tokenType chroma.TokenType,
	theme CodeTheme,
	baseStyle tcell.Style,
) {
	if segment == "" {
		return
	}

	lastIndex := len(*lines) - 1
	line := (*lines)[lastIndex]
	style := styleForToken(tokenType, theme, baseStyle)
	line.Spans = append(line.Spans, Span{Text: segment, Style: style})
	line.Text += segment
	(*lines)[lastIndex] = line
}

func styleForToken(tokenType chroma.TokenType, theme CodeTheme, baseStyle tcell.Style) tcell.Style {
	if tokenType.InCategory(chroma.Text) {
		return baseStyle
	}

	if tokenType.InCategory(chroma.Comment) {
		return baseStyle.Foreground(theme.Muted).Italic(true)
	}

	if tokenType.InCategory(chroma.Keyword) {
		return baseStyle.Foreground(theme.Accent)
	}

	if tokenType.InCategory(chroma.Name) {
		return baseStyle.Foreground(codeNameColor(tokenType, theme))
	}

	if tokenType.InSubCategory(chroma.LiteralString) {
		return baseStyle.Foreground(theme.Success)
	}

	if tokenType.InSubCategory(chroma.LiteralNumber) {
		return baseStyle.Foreground(theme.Text)
	}

	if tokenType.InCategory(chroma.Operator) || tokenType.InCategory(chroma.Punctuation) {
		return baseStyle.Foreground(theme.Dim)
	}

	if tokenType.InCategory(chroma.Generic) {
		return baseStyle.Foreground(codeGenericColor(tokenType, theme))
	}

	return baseStyle.Foreground(theme.Warning)
}

// WrapLines wraps rich lines to width cells, trimming wrapping whitespace.
func WrapLines(lines []Line, width int) []Line {
	return wrapLinesWithMode(lines, width, false)
}

// WrapCodeLines wraps code-like rich lines without letting indentation hide content.
func WrapCodeLines(lines []Line, width int) []Line {
	if width <= 0 {
		return []Line{}
	}

	wrapped := []Line{}
	for _, line := range lines {
		wrapped = append(wrapped, wrapCodeLine(line, width)...)
	}

	return wrapped
}

func wrapCodeLine(line Line, width int) []Line {
	indent, body := splitLeadingWhitespace(line.styledSegments())
	if len(body) == 0 {
		return line.Wrap(width)
	}

	indentWidth := codeIndentWidth(indent, width)
	bodyWidth := max(1, width-indentWidth)

	bodyLines := lineFromStyledSegments(body, line.Style).Wrap(bodyWidth)
	if indentWidth == 0 {
		return bodyLines
	}

	lines := make([]Line, 0, len(bodyLines))
	for _, bodyLine := range bodyLines {
		segments := appendCodeIndent(bodyLine.styledSegments(), indentWidth, line.Style)
		lines = append(lines, lineFromStyledSegments(segments, line.Style))
	}

	return lines
}

func splitLeadingWhitespace(segments []styledSegment) (indent, body []styledSegment) {
	for index, segment := range segments {
		if strings.Trim(segment.Text, " \t") != "" {
			return segments[:index], segments[index:]
		}
	}

	return segments, nil
}

func codeIndentWidth(indent []styledSegment, width int) int {
	indentWidth := 0
	for _, segment := range indent {
		indentWidth += segment.Width
	}

	return min(indentWidth, max(0, width-minCodeBodyWidth))
}

func appendCodeIndent(segments []styledSegment, width int, style tcell.Style) []styledSegment {
	if width <= 0 {
		return segments
	}

	indented := make([]styledSegment, 0, len(segments)+1)
	indented = append(indented, styledSegment{Text: strings.Repeat(" ", width), Width: width, Style: style})

	return append(indented, segments...)
}

func wrapLinesWithMode(lines []Line, width int, preserveWhitespace bool) []Line {
	if width <= 0 {
		return []Line{}
	}

	wrapped := []Line{}

	for _, line := range lines {
		if preserveWhitespace {
			wrapped = append(wrapped, line.WrapPreserveWhitespace(width)...)

			continue
		}

		wrapped = append(wrapped, line.Wrap(width)...)
	}

	return wrapped
}

func codeNameColor(tokenType chroma.TokenType, theme CodeTheme) tcell.Color {
	if tokenType == chroma.NameFunction {
		return theme.Success
	}

	if tokenType == chroma.NameClass || tokenType == chroma.NameNamespace {
		return theme.Warning
	}

	if tokenType == chroma.NameVariable || tokenType == chroma.NameBuiltin {
		return theme.Text
	}

	return theme.Text
}

func codeGenericColor(tokenType chroma.TokenType, theme CodeTheme) tcell.Color {
	if tokenType == chroma.GenericInserted {
		return theme.DiffAdd
	}

	if tokenType == chroma.GenericDeleted {
		return theme.DiffDel
	}

	if tokenType == chroma.GenericHeading {
		return theme.Accent
	}

	return theme.Text
}

func trimTrailingEmptyCodeLine(lines []Line, baseStyle tcell.Style) []Line {
	for len(lines) > 1 {
		last := lines[len(lines)-1]
		if last.Text != "" || last.Style != baseStyle || len(last.Spans) > 0 {
			break
		}

		lines = lines[:len(lines)-1]
	}

	return lines
}
