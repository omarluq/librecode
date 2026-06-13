package terminal

import (
	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/terminal/rendertext"
	"github.com/omarluq/librecode/tui"
)

const (
	markdownIndent     = " "
	markdownBullet     = "• "
	markdownCodePrefix = "  "
)

func (app *App) renderMarkdown(content string, width int) []rendertext.Line {
	view := tui.MarkdownView{
		Text: content,
		Styles: tui.MarkdownStyles{
			Text:      app.theme.style(colorText).Bold(true),
			Accent:    app.theme.style(colorAccent),
			Muted:     app.theme.style(colorBorderMuted),
			Code:      markdownCodeStyle(app.theme),
			CodeTheme: codeTheme(app.theme),
		},
	}

	return view.Render(width, 1_000_000)
}

func markdownCodeStyle(theme terminalTheme) tcell.Style {
	return theme.background(colorCodeBg).Foreground(theme.colors[colorCodeText])
}
