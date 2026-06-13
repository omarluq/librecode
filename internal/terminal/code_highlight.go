package terminal

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/terminal/rendertext"
	"github.com/omarluq/librecode/tui"
)

func syntaxHighlightedCodeLines(language, text string, theme terminalTheme, baseStyle tcell.Style) []rendertext.Line {
	return tui.SyntaxHighlightedCodeLines(language, text, codeTheme(theme), baseStyle)
}

func diffStyledLines(text string, theme terminalTheme, baseStyle tcell.Style) []rendertext.Line {
	return tui.DiffStyledLines(text, codeTheme(theme), baseStyle)
}

func codeTheme(theme terminalTheme) tui.CodeTheme {
	return tui.CodeTheme{
		Text:    theme.colors[colorCodeText],
		Accent:  theme.colors[colorAccent],
		Success: theme.colors[colorSuccess],
		Warning: theme.colors[colorWarning],
		Dim:     theme.colors[colorDim],
		Muted:   theme.colors[colorMuted],
		DiffAdd: theme.colors[colorDiffAdd],
		DiffDel: theme.colors[colorDiffDelete],
	}
}

func styleForToken(tokenType chroma.TokenType, theme terminalTheme, baseStyle tcell.Style) tcell.Style {
	category := tokenType.Category()
	switch {
	case category == chroma.Comment:
		return baseStyle.Foreground(codeCommentColor(theme)).Italic(true)
	case category == chroma.Keyword:
		return baseStyle.Foreground(codeKeywordColor(theme))
	case category == chroma.Name:
		return baseStyle.Foreground(codeNameColor(tokenType, theme))
	case category == chroma.LiteralString:
		return baseStyle.Foreground(codeStringColor(theme))
	case category == chroma.LiteralNumber:
		return baseStyle.Foreground(codeNumberColor(theme))
	case category == chroma.Operator || category == chroma.Punctuation:
		return baseStyle.Foreground(codeOperatorColor(theme))
	case category == chroma.Generic:
		return baseStyle.Foreground(codeGenericColor(tokenType, theme))
	default:
		return baseStyle.Foreground(codeTypeColor(theme))
	}
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

func codeKeywordColor(theme terminalTheme) tcell.Color { return theme.colors[colorAccent] }

func codeFunctionColor(theme terminalTheme) tcell.Color { return theme.colors[colorSuccess] }

func codeTypeColor(theme terminalTheme) tcell.Color { return theme.colors[colorWarning] }

func codeVariableColor(theme terminalTheme) tcell.Color { return theme.colors[colorCodeText] }

func codeNumberColor(theme terminalTheme) tcell.Color { return theme.colors[colorCodeText] }

func codeStringColor(theme terminalTheme) tcell.Color { return theme.colors[colorSuccess] }

func codeOperatorColor(theme terminalTheme) tcell.Color { return theme.colors[colorDim] }

func codeCommentColor(theme terminalTheme) tcell.Color { return theme.colors[colorMuted] }

func trimTrailingEmptyCodeLine(lines []rendertext.Line, baseStyle tcell.Style) []rendertext.Line {
	for len(lines) > 1 {
		last := lines[len(lines)-1]
		if last.Text != strings.Repeat(" ", 2) || last.Style != baseStyle || len(last.Spans) > 0 {
			break
		}
		lines = lines[:len(lines)-1]
	}

	return lines
}
