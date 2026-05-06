package terminal

import (
	"strings"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/vinfo"
)

const welcomeMessagePrefix = "__librecode_welcome__\n"

var welcomeArt = []string{
	" ██╗     ██╗██████╗ ██████╗ ███████╗ ██████╗ ██████╗ ██████╗ ███████╗",
	" ██║     ██║██╔══██╗██╔══██╗██╔════╝██╔════╝██╔═══██╗██╔══██╗██╔════╝",
	" ██║     ██║██████╔╝██████╔╝█████╗  ██║     ██║   ██║██║  ██║█████╗  ",
	" ██║     ██║██╔══██╗██╔══██╗██╔══╝  ██║     ██║   ██║██║  ██║██╔══╝  ",
	" ███████╗██║██████╔╝██║  ██║███████╗╚██████╗╚██████╔╝██████╔╝███████╗",
	" ╚══════╝╚═╝╚═════╝ ╚═╝  ╚═╝╚══════╝ ╚═════╝ ╚═════╝ ╚═════╝ ╚══════╝",
}

func (app *App) addWelcomeMessage() {
	app.addSystemMessage(welcomeMessagePrefix + strings.Join(welcomeBodyLines(app.cwd), "\n"))
}

func (app *App) renderWelcomeMessage(width int, content string) []styledLine {
	body := strings.TrimPrefix(content, welcomeMessagePrefix)
	style := app.theme.background(colorCustomMessageBg)
	bodyLines := strings.Split(body, "\n")
	lines := make([]styledLine, 0, len(bodyLines)+2)
	lines = append(lines, styledLine{Style: style.Bold(true), Text: boxTop(width, "welcome")})
	for index, line := range bodyLines {
		lineStyle := app.welcomeLineStyle(index, line, style)
		lines = append(lines, styledLine{Style: lineStyle, Text: boxedBodyLine(width, line)})
	}
	lines = append(lines, styledLine{Style: style.Bold(true), Text: boxBottom(width)})

	return lines
}

func (app *App) showWelcomeOnly() bool {
	return app.mode == modeChat &&
		len(app.messages) == 1 &&
		isWelcomeMessage(app.messages[0].Content) &&
		app.statusMessage == ""
}

func isWelcomeMessage(content string) bool {
	return strings.HasPrefix(content, welcomeMessagePrefix)
}

func (app *App) welcomeLineStyle(index int, line string, baseStyle tcell.Style) tcell.Style {
	if index < len(welcomeArt) {
		colors := []colorToken{
			colorBorderAccent,
			colorAccent,
			colorSuccess,
			colorWarning,
			colorDiffDelete,
			colorBorderMuted,
		}
		return app.theme.background(colorCustomMessageBg).
			Foreground(app.theme.colors[colors[index%len(colors)]]).
			Bold(true)
	}
	if strings.HasPrefix(line, "Type /") || strings.HasPrefix(line, "Press Ctrl+D") {
		return app.theme.background(colorCustomMessageBg).Foreground(app.theme.colors[colorAccent]).Bold(true)
	}
	if strings.HasPrefix(line, "version") || strings.HasPrefix(line, "workspace") {
		return app.theme.background(colorCustomMessageBg).Foreground(app.theme.colors[colorMuted])
	}

	return baseStyle
}

func welcomeBodyLines(cwd string) []string {
	lines := make([]string, 0, len(welcomeArt)+6)
	lines = append(lines, welcomeArt...)
	lines = append(lines,
		"",
		"version   "+vinfo.String(),
		"workspace "+cwd,
		"",
		"Type /hotkeys for shortcuts",
		"Type /quit to exit  ·  Press Ctrl+D on an empty prompt to exit",
	)

	return lines
}
