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
	style := app.theme.background(colorCustomMessageBg)
	lines := make([]styledLine, 0, len(bodyLines)+2)
	lines = append(lines, styledLine{Style: app.welcomeBorderStyle(), Text: boxTop(width, "welcome")})
	for _, line := range bodyLines {
		lines = append(lines, styledLine{Style: style, Text: boxedBodyLine(width, line)})
	}
	lines = append(lines, styledLine{Style: app.welcomeBorderStyle(), Text: boxBottom(width)})

	return lines
}

func (app *App) drawWelcomeOnly(width, height, row int) int {
	bodyLines := welcomeBodyLines(app.cwd)
	availableRows := max(1, height-row-footerReserve())
	maxBodyRows := max(0, availableRows-2)
	if len(bodyLines) > maxBodyRows {
		bodyLines = bodyLines[:maxBodyRows]
	}
	app.writeWelcomeBorder(row, width, boxTop(width, "welcome"))
	row++
	for index, line := range bodyLines {
		app.writeWelcomeBodyLine(row, width, index, line)
		row++
	}
	app.writeWelcomeBorder(row, width, boxBottom(width))

	return row + 1
}

func (app *App) writeWelcomeBorder(row, width int, text string) {
	writeLine(app.screen, row, width, text, app.welcomeBorderStyle())
}

func (app *App) writeWelcomeBodyLine(row, width, lineIndex int, content string) {
	if row < 0 {
		return
	}
	borderStyle := app.welcomeBorderStyle()
	bodyStyle := app.welcomeBodyStyle(lineIndex, content)
	innerWidth := max(1, width-4)
	padded := []rune(padRight(content, innerWidth))
	app.screen.SetContent(0, row, '│', nil, borderStyle)
	app.screen.SetContent(1, row, ' ', nil, borderStyle)
	for index := 0; index < innerWidth; index++ {
		value := ' '
		if index < len(padded) {
			value = padded[index]
		}
		style := bodyStyle
		if lineIndex < len(welcomeArt) && value != ' ' {
			style = app.welcomeArtStyle(index, innerWidth)
		}
		app.screen.SetContent(index+2, row, value, nil, style)
	}
	app.screen.SetContent(width-2, row, ' ', nil, borderStyle)
	app.screen.SetContent(width-1, row, '│', nil, borderStyle)
}

func (app *App) showWelcomeOnly() bool {
	return app.mode == modeChat &&
		len(app.messages) == 1 &&
		isWelcomeMessage(app.messages[0].Content)
}

func isWelcomeMessage(content string) bool {
	return strings.HasPrefix(content, welcomeMessagePrefix)
}

func (app *App) welcomeBorderStyle() tcell.Style {
	return app.theme.background(colorCustomMessageBg).
		Foreground(app.theme.colors[colorBorderMuted]).
		Bold(true)
}

func (app *App) welcomeBodyStyle(index int, line string) tcell.Style {
	if index < len(welcomeArt) {
		return app.theme.background(colorCustomMessageBg).Foreground(app.theme.colors[colorAccent]).Bold(true)
	}
	if strings.HasPrefix(line, "Type /") || strings.HasPrefix(line, "Press Ctrl+D") {
		return app.theme.background(colorCustomMessageBg).Foreground(app.theme.colors[colorAccent]).Bold(true)
	}
	if strings.HasPrefix(line, "version") || strings.HasPrefix(line, "workspace") {
		return app.theme.background(colorCustomMessageBg).Foreground(app.theme.colors[colorMuted])
	}

	return app.theme.background(colorCustomMessageBg)
}

func (app *App) welcomeArtStyle(column, width int) tcell.Style {
	return app.theme.background(colorCustomMessageBg).
		Foreground(welcomeGradientColor(column, width)).
		Bold(true)
}

func welcomeGradientColor(column, width int) tcell.Color {
	palette := []tcell.Color{
		hexColor(0x00d7ff),
		hexColor(0x5f87ff),
		hexColor(0x8a7dff),
		hexColor(0xff5fd7),
		hexColor(0xff875f),
		hexColor(0xffd75f),
		hexColor(0xb5bd68),
	}
	if width <= 1 {
		return palette[0]
	}
	index := max(0, column) * (len(palette) - 1) / max(1, width-1)

	return palette[min(index, len(palette)-1)]
}

func welcomeBodyLines(cwd string) []string {
	lines := make([]string, 0, len(welcomeArt)+7)
	lines = append(lines, welcomeArt...)
	lines = append(lines,
		"",
		"version   "+vinfo.String(),
		"workspace "+cwd,
		"",
		"Type /hotkeys for shortcuts",
		"Type /quit to exit",
		"Press Ctrl+D on an empty prompt to exit",
	)

	return lines
}

func welcomeLinesFromContent(content string) []string {
	body := strings.TrimPrefix(content, welcomeMessagePrefix)

	return strings.Split(body, "\n")
}
