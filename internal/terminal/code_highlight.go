package terminal

import (
	"errors"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/terminal/rendertext"
)

func syntaxHighlightedCodeLines(language, text string, theme terminalTheme, baseStyle tcell.Style) []rendertext.Line {
	iterator, err := codeTokenIterator(language, text)
	if err != nil {
		return codeStyledLines(text, baseStyle)
	}

	lines := codeLinesFromTokens(iterator.Tokens(), theme, baseStyle)
	if len(lines) == 0 {
		return []rendertext.Line{rendertext.NewLine(baseStyle, markdownCodePrefix)}
	}

	return lines
}

func codeTokenIterator(language, text string) (chroma.Iterator, error) {
	lexer := lexers.Get(strings.TrimSpace(language))
	if lexer != nil {
		return tokenizeCode(lexer, text)
	}

	if strings.TrimSpace(language) != "" {
		return nil, terminalError(errors.New("code lexer not found"), "find code lexer")
	}

	return analyzedCodeIterator(text)
}

func tokenizeCode(lexer chroma.Lexer, text string) (chroma.Iterator, error) {
	iterator, err := chroma.Coalesce(lexer).Tokenise(nil, text)
	if err != nil {
		return nil, terminalError(err, "tokenize highlighted code")
	}

	return iterator, nil
}

func analyzedCodeIterator(text string) (chroma.Iterator, error) {
	highest := float32(0)

	for _, lexer := range lexers.GlobalLexerRegistry.Lexers {
		weight := lexer.AnalyseText(text)
		if weight > highest {
			highest = weight

			continue
		}
	}

	for _, lexer := range lexers.GlobalLexerRegistry.Lexers {
		if lexer.AnalyseText(text) == highest && highest > 0 {
			return tokenizeCode(lexer, text)
		}
	}

	return nil, terminalError(errors.New("code lexer not found"), "find code lexer")
}

func codeStyledLines(text string, baseStyle tcell.Style) []rendertext.Line {
	parts := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(parts) == 0 {
		return []rendertext.Line{rendertext.NewLine(baseStyle, markdownCodePrefix)}
	}

	lines := make([]rendertext.Line, 0, len(parts))
	for _, part := range parts {
		lines = append(lines, rendertext.NewLine(baseStyle, markdownCodePrefix+part))
	}

	return lines
}

func diffStyledLines(text string, theme terminalTheme, baseStyle tcell.Style) []rendertext.Line {
	parts := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(parts) == 0 {
		return nil
	}

	lines := make([]rendertext.Line, 0, len(parts))
	for _, part := range parts {
		style := baseStyle
		if strings.HasPrefix(part, "+") {
			style = baseStyle.Foreground(theme.colors[colorDiffAdd])
		} else if strings.HasPrefix(part, "-") {
			style = baseStyle.Foreground(theme.colors[colorDiffDelete])
		}

		lines = append(lines, rendertext.NewLine(style, part))
	}

	return lines
}

func codeLinesFromTokens(tokens []chroma.Token, theme terminalTheme, baseStyle tcell.Style) []rendertext.Line {
	lines := []rendertext.Line{rendertext.NewLine(baseStyle, markdownCodePrefix)}

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
				lines = append(lines, rendertext.NewLine(baseStyle, markdownCodePrefix))

				continue
			}

			appendCodeSegment(&lines, segment, token.Type, theme, baseStyle)
		}
	}

	return trimTrailingEmptyCodeLine(lines, baseStyle)
}

func appendCodeSegment(
	lines *[]rendertext.Line,
	segment string,
	tokenType chroma.TokenType,
	theme terminalTheme,
	baseStyle tcell.Style,
) {
	if segment == "" {
		return
	}

	lastIndex := len(*lines) - 1
	line := (*lines)[lastIndex]
	style := styleForToken(tokenType, theme, baseStyle)
	line.Spans = append(line.Spans, rendertext.Span{Text: segment, Style: style})
	line.Text += segment
	(*lines)[lastIndex] = line
}

func styleForToken(tokenType chroma.TokenType, theme terminalTheme, baseStyle tcell.Style) tcell.Style {
	category := tokenType.Category()

	if category == chroma.Comment {
		return baseStyle.Foreground(codeCommentColor(theme)).Italic(true)
	}

	if category == chroma.Keyword {
		return baseStyle.Foreground(codeKeywordColor(theme))
	}

	if category == chroma.Name {
		return baseStyle.Foreground(codeNameColor(tokenType, theme))
	}

	if category == chroma.LiteralString {
		return baseStyle.Foreground(codeStringColor(theme))
	}

	if category == chroma.LiteralNumber {
		return baseStyle.Foreground(codeNumberColor(theme))
	}

	if category == chroma.Operator || category == chroma.Punctuation {
		return baseStyle.Foreground(codeOperatorColor(theme))
	}

	if category == chroma.Generic {
		return baseStyle.Foreground(codeGenericColor(tokenType, theme))
	}

	return baseStyle.Foreground(codeTypeColor(theme))
}

func codeNameColor(tokenType chroma.TokenType, theme terminalTheme) tcell.Color {
	if tokenType == chroma.NameFunction {
		return codeFunctionColor(theme)
	}

	if tokenType == chroma.NameClass || tokenType == chroma.NameNamespace {
		return codeTypeColor(theme)
	}

	if tokenType == chroma.NameVariable || tokenType == chroma.NameBuiltin {
		return codeVariableColor(theme)
	}

	return theme.colors[colorCodeText]
}

func codeLiteralColor(tokenType chroma.TokenType, theme terminalTheme) tcell.Color {
	if tokenType == chroma.LiteralString {
		return codeStringColor(theme)
	}

	if tokenType == chroma.LiteralNumber {
		return codeNumberColor(theme)
	}

	return theme.colors[colorCodeText]
}

func codeGenericColor(tokenType chroma.TokenType, theme terminalTheme) tcell.Color {
	if tokenType == chroma.GenericInserted {
		return theme.colors[colorDiffAdd]
	}

	if tokenType == chroma.GenericDeleted {
		return theme.colors[colorDiffDelete]
	}

	if tokenType == chroma.GenericHeading {
		return theme.colors[colorAccent]
	}

	return theme.colors[colorCodeText]
}

func codeKeywordColor(theme terminalTheme) tcell.Color {
	return theme.colors[colorAccent]
}

func codeFunctionColor(theme terminalTheme) tcell.Color {
	return theme.colors[colorSuccess]
}

func codeTypeColor(theme terminalTheme) tcell.Color {
	return theme.colors[colorWarning]
}

func codeVariableColor(theme terminalTheme) tcell.Color {
	return theme.colors[colorCodeText]
}

func codeNumberColor(theme terminalTheme) tcell.Color {
	return theme.colors[colorCodeText]
}

func codeStringColor(theme terminalTheme) tcell.Color {
	return theme.colors[colorSuccess]
}

func codeOperatorColor(theme terminalTheme) tcell.Color {
	return theme.colors[colorDim]
}

func codeCommentColor(theme terminalTheme) tcell.Color {
	return theme.colors[colorMuted]
}

func trimTrailingEmptyCodeLine(lines []rendertext.Line, baseStyle tcell.Style) []rendertext.Line {
	if len(lines) > 1 {
		last := lines[len(lines)-1]
		if last.Text == markdownCodePrefix && len(last.Spans) == 0 && last.Style == baseStyle {
			return lines[:len(lines)-1]
		}
	}

	return lines
}
