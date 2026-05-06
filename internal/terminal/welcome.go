package terminal

import (
	"strings"

	"github.com/omarluq/librecode/internal/vinfo"
)

const welcomeMessagePrefix = "__librecode_welcome__\n"

func (app *App) addWelcomeMessage() {
	app.addSystemMessage(welcomeMessagePrefix + strings.Join(welcomeBodyLines(app.cwd), "\n"))
}

func (app *App) renderWelcomeMessage(width int, content string) []styledLine {
	body := strings.TrimPrefix(content, welcomeMessagePrefix)
	style := app.theme.background(colorCustomMessageBg)
	bodyLines := strings.Split(body, "\n")
	lines := make([]styledLine, 0, len(bodyLines)+2)
	lines = append(lines, styledLine{Style: style.Bold(true), Text: boxTop(width, "welcome")})
	for _, line := range bodyLines {
		lines = append(lines, styledLine{Style: style, Text: boxedBodyLine(width, line)})
	}
	lines = append(lines, styledLine{Style: style.Bold(true), Text: boxBottom(width)})

	return lines
}

func isWelcomeMessage(content string) bool {
	return strings.HasPrefix(content, welcomeMessagePrefix)
}

func welcomeBodyLines(cwd string) []string {
	lines := []string{
		" _ _ _                          _     ",
		"| (_) |__  _ __ ___  ___ ___   __| | ___ ",
		"| | | '_ \\| '__/ _ \\/ __/ _ \\ / _` |/ _ \\",
		"| | | |_) | | |  __/ (_| (_) | (_| |  __/",
		"|_|_|_.__/|_|  \\___|\\___\\___/ \\__,_|\\___|",
		"",
		"version  " + vinfo.String(),
		"workspace " + cwd,
		"",
		"Type /hotkeys for shortcuts",
		"Type /quit to exit",
		"Press Ctrl+D on an empty prompt to exit",
	}

	return lines
}
