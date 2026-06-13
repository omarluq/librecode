package tui

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/gdamore/tcell/v3"
)

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
	Language string
	Text     string
	Theme    CodeTheme
}

// Render returns highlighted code lines.
func (block *CodeBlock) Render(width, height int) []Line {
	if block == nil || width <= 0 || height <= 0 {
		return []Line{}
	}

	lines := SyntaxHighlightedCodeLines(block.Language, block.Text, block.Theme, block.Style)
	for index := range lines {
		lines[index] = lines[index].Truncate(width)
	}

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

	lines := DiffStyledLines(view.Text, view.Theme, view.Style)
	for index := range lines {
		lines[index] = lines[index].Truncate(width)
	}

	return Tail(lines, height)
}

// Draw draws diff lines.
func (view *DiffView) Draw(screen ContentSetter, rect Rect) {
	DrawLines(screen, rect, view.Render(rect.Width, rect.Height))
}

// SyntaxHighlightedCodeLines returns syntax-highlighted code lines.
func SyntaxHighlightedCodeLines(language, text string, theme CodeTheme, baseStyle tcell.Style) []Line {
	iterator, ok := codeTokenIterator(language, text)
	if !ok {
		return codeStyledLines(text, baseStyle)
	}

	lines := codeLinesFromTokens(iterator.Tokens(), theme, baseStyle)
	if len(lines) == 0 {
		return []Line{NewLine(baseStyle, "  ")}
	}

	return lines
}

func codeTokenIterator(language, text string) (chroma.Iterator, bool) {
	lexer := lexers.Get(strings.TrimSpace(language))
	if lexer != nil {
		return tokenizeCode(lexer, text)
	}

	if strings.TrimSpace(language) != "" {
		return nil, false
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
		return []Line{NewLine(baseStyle, "  ")}
	}

	lines := make([]Line, 0, len(parts))
	for _, part := range parts {
		lines = append(lines, NewLine(baseStyle, "  "+part))
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
	lines := []Line{NewLine(baseStyle, "  ")}

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
				lines = append(lines, NewLine(baseStyle, "  "))

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
	category := tokenType.Category()
	switch category { //nolint:exhaustive // Chroma token categories are extensible; unknown categories use Warning.
	case chroma.Comment:
		return baseStyle.Foreground(theme.Muted).Italic(true)
	case chroma.Keyword:
		return baseStyle.Foreground(theme.Accent)
	case chroma.Name:
		return baseStyle.Foreground(codeNameColor(tokenType, theme))
	case chroma.LiteralString:
		return baseStyle.Foreground(theme.Success)
	case chroma.LiteralNumber:
		return baseStyle.Foreground(theme.Text)
	case chroma.Operator, chroma.Punctuation:
		return baseStyle.Foreground(theme.Dim)
	case chroma.Generic:
		return baseStyle.Foreground(codeGenericColor(tokenType, theme))
	default:
		return baseStyle.Foreground(theme.Warning)
	}
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
		if last.Text != "  " || last.Style != baseStyle || len(last.Spans) > 0 {
			break
		}

		lines = lines[:len(lines)-1]
	}

	return lines
}
