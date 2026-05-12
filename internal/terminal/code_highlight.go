package terminal

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/gdamore/tcell/v3"
)

func syntaxHighlightedCodeLines(language, text string, theme terminalTheme, baseStyle tcell.Style) []styledLine {
	lexer := codeLexer(language, text)
	if lexer == nil {
		return codeStyledLines(text, baseStyle)
	}
	iterator, err := lexer.Tokenise(nil, text)
	if err != nil {
		return codeStyledLines(text, baseStyle)
	}

	lines := codeLinesFromTokens(iterator.Tokens(), theme, baseStyle)
	if len(lines) == 0 {
		return []styledLine{newStyledLine(baseStyle, markdownCodePrefix)}
	}

	return lines
}

func codeLexer(language, text string) chroma.Lexer {
	language = strings.TrimSpace(language)
	if language != "" {
		if lexer := lexers.Get(language); lexer != nil {
			return chroma.Coalesce(lexer)
		}
	}
	lexer := lexers.Analyse(text) //nolint:misspell // Chroma uses British spelling in its public API.
	if lexer == nil {
		return nil
	}

	return chroma.Coalesce(lexer)
}

func codeLinesFromTokens(tokens []chroma.Token, theme terminalTheme, baseStyle tcell.Style) []styledLine {
	lines := []styledLine{newStyledLine(baseStyle, markdownCodePrefix)}
	for _, token := range tokens {
		if token.Type == chroma.EOFType {
			break
		}
		style := styleForToken(token.Type, theme, baseStyle)
		appendTokenToCodeLines(&lines, token.Value, style, baseStyle)
	}
	if len(lines) > 1 && strings.TrimPrefix(lines[len(lines)-1].Text, markdownCodePrefix) == "" {
		lines = lines[:len(lines)-1]
	}

	return lines
}

func appendTokenToCodeLines(lines *[]styledLine, value string, style, baseStyle tcell.Style) {
	for {
		before, after, found := strings.Cut(value, "\n")
		appendStyledTextToLastLine(lines, before, style)
		if !found {
			return
		}
		*lines = append(*lines, newStyledLine(baseStyle, markdownCodePrefix))
		value = after
	}
}

func appendStyledTextToLastLine(lines *[]styledLine, text string, style tcell.Style) {
	if text == "" {
		return
	}
	index := len(*lines) - 1
	line := (*lines)[index]
	line.Spans = append(line.Spans, styledSpan{Style: style, Text: text})
	line.Text += text
	(*lines)[index] = line
}

func styleForToken(token chroma.TokenType, theme terminalTheme, baseStyle tcell.Style) tcell.Style {
	style := baseStyle.Foreground(colorForToken(token, theme))
	//nolint:exhaustive // Categories are broad ranges; default handles all uncategorized tokens.
	switch token.Category() {
	case chroma.Keyword:
		return style.Bold(true)
	case chroma.Name:
		if token.InCategory(chroma.NameFunction) || token.InCategory(chroma.NameClass) {
			return style.Bold(true)
		}
	case chroma.Comment:
		return style.Italic(true)
	}

	return style
}

func colorForToken(token chroma.TokenType, theme terminalTheme) tcell.Color {
	//nolint:exhaustive // Categories are broad ranges; default handles all uncategorized tokens.
	switch token.Category() {
	case chroma.Keyword:
		return codeKeywordColor(theme)
	case chroma.Name:
		return codeNameColor(token, theme)
	case chroma.Literal:
		return codeLiteralColor(token, theme)
	case chroma.Operator:
		return codeOperatorColor(theme)
	case chroma.Comment:
		return theme.colors[colorDim]
	case chroma.Generic:
		return codeGenericColor(token, theme)
	default:
		return theme.colors[colorCodeText]
	}
}

func codeNameColor(token chroma.TokenType, theme terminalTheme) tcell.Color {
	switch {
	case token.InCategory(chroma.NameFunction):
		return codeFunctionColor(theme)
	case token.InCategory(chroma.NameClass), token.InCategory(chroma.NameBuiltin):
		return codeTypeColor(theme)
	case token.InCategory(chroma.NameVariable), token.InCategory(chroma.NameConstant):
		return codeVariableColor(theme)
	default:
		return theme.colors[colorCodeText]
	}
}

func codeLiteralColor(token chroma.TokenType, theme terminalTheme) tcell.Color {
	switch {
	case token.InCategory(chroma.LiteralString):
		return codeStringColor(theme)
	case token.InCategory(chroma.LiteralNumber):
		return codeNumberColor(theme)
	default:
		return theme.colors[colorCodeText]
	}
}

func codeGenericColor(token chroma.TokenType, theme terminalTheme) tcell.Color {
	switch {
	case token.InCategory(chroma.GenericInserted):
		return theme.colors[colorDiffAdd]
	case token.InCategory(chroma.GenericDeleted):
		return theme.colors[colorDiffDelete]
	case token.InCategory(chroma.GenericHeading):
		return theme.colors[colorAccent]
	default:
		return theme.colors[colorCodeText]
	}
}

func codeKeywordColor(theme terminalTheme) tcell.Color {
	if theme.name == themeNameLight {
		return hexColor(0xcf222e)
	}

	return hexColor(0xff7b72)
}

func codeFunctionColor(theme terminalTheme) tcell.Color {
	if theme.name == themeNameLight {
		return hexColor(0x8250df)
	}

	return hexColor(0xd2a8ff)
}

func codeTypeColor(theme terminalTheme) tcell.Color {
	if theme.name == themeNameLight {
		return hexColor(0x953800)
	}

	return hexColor(0xffd8a8)
}

func codeVariableColor(theme terminalTheme) tcell.Color {
	if theme.name == themeNameLight {
		return hexColor(0x0550ae)
	}

	return hexColor(0x79c0ff)
}

func codeStringColor(theme terminalTheme) tcell.Color {
	if theme.name == themeNameLight {
		return hexColor(0x0a3069)
	}

	return hexColor(0xa5d6ff)
}

func codeNumberColor(theme terminalTheme) tcell.Color {
	if theme.name == themeNameLight {
		return hexColor(0x0550ae)
	}

	return hexColor(0x79c0ff)
}

func codeOperatorColor(theme terminalTheme) tcell.Color {
	if theme.name == themeNameLight {
		return hexColor(0x116329)
	}

	return hexColor(0x7ee787)
}
