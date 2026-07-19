package terminal

import (
	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/tui"
)

const markdownRenderMaxHeight = 1_000_000

func (app *App) renderMarkdown(content string, width int) []tui.Line {
	return app.renderMarkdownDetailed(content, width).Lines
}

func (app *App) renderMarkdownDetailed(content string, width int) tui.MarkdownRender {
	view := tui.MarkdownView{
		Text: content,
		Styles: tui.MarkdownStyles{
			Text:      app.theme.style(colorText),
			Accent:    app.theme.style(colorAccent),
			Muted:     app.theme.style(colorBorderMuted),
			Code:      markdownCodeStyle(app.theme),
			CodeTheme: codeTheme(app.theme),
		},
		Engine: &app.renderer.Markdown,
		Lexer:  &app.renderer.Lexer,
	}

	return view.RenderDetailed(width, markdownRenderMaxHeight)
}

func markdownCodeStyle(theme terminalTheme) tcell.Style {
	return theme.background(colorCodeBg).Foreground(theme.colors[colorCodeText])
}
