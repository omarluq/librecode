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
	bodyLines := welcomeLinesFromContent(content)
	lines := make([]styledLine, 0, len(bodyLines)+1)
	for index, line := range bodyLines {
		lineStyle := app.welcomeBodyStyle(index, line)
		lines = append(lines, styledLine{Style: lineStyle, Text: padRight(line, width)})
	}
	lines = append(lines, styledLine{Style: app.theme.style(colorDim), Text: ""})

	return lines
}

func (app *App) drawWelcomeOnly(width, height, row int) int {
	bodyLines := welcomeBodyLines(app.cwd)
	availableRows := max(1, height-row-footerReserve())
	if len(bodyLines) > availableRows {
		bodyLines = bodyLines[:availableRows]
	}
	for index, line := range bodyLines {
		app.writeWelcomeLine(row, width, index, line)
		row++
	}

	return row + 1
}

func (app *App) writeWelcomeLine(row, width, lineIndex int, content string) {
	writeLine(app.frame, row, width, padRight(content, width), app.welcomeBodyStyle(lineIndex, content))
}

func (app *App) showWelcomeOnly() bool {
	return app.mode == modeChat &&
		len(app.messages) == 1 &&
		isWelcomeMessage(app.messages[0].Content)
}

func isWelcomeMessage(content string) bool {
	return strings.HasPrefix(content, welcomeMessagePrefix)
}

func (app *App) welcomeBodyStyle(index int, line string) tcell.Style {
	if index < len(welcomeArt) {
		return app.theme.background(colorCustomMessageBg).
			Foreground(app.theme.colors[colorBorderAccent]).
			Bold(true)
	}
	if strings.HasPrefix(line, "Type /") || strings.HasPrefix(line, "Press Ctrl+C") {
		return app.theme.background(colorCustomMessageBg).Foreground(app.theme.colors[colorAccent]).Bold(true)
	}
	if strings.HasPrefix(line, "version") || strings.HasPrefix(line, "workspace") {
		return app.theme.background(colorCustomMessageBg).Foreground(app.theme.colors[colorMuted])
	}

	return app.theme.background(colorCustomMessageBg)
}

func welcomeBodyLines(cwd string) []string {
	lines := make([]string, 0, len(welcomeArt)+8)
	lines = append(lines, welcomeArt...)
	lines = append(lines,
		"",
		"version   "+vinfo.String(),
		"workspace "+cwd,
		"",
		"Type /hotkeys for shortcuts",
		"Type /quit to exit",
		"Press Ctrl+C twice to exit",
	)

	return lines
}

func welcomeLinesFromContent(content string) []string {
	body := strings.TrimPrefix(content, welcomeMessagePrefix)

	return strings.Split(body, "\n")
}
