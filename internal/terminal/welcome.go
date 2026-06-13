package terminal

import (
	"github.com/omarluq/librecode/internal/terminal/rendertext"
	"strings"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/vinfo"
)

const (
	welcomeMessagePrefix = "__librecode_welcome__\n"
	welcomeTopMarginRows = 1
	welcomePaddingX      = 2
	welcomePaddingY      = 1
)

func welcomeArt() []string {
	return []string{
		" ██╗     ██╗██████╗ ██████╗ ███████╗ ██████╗ ██████╗ ██████╗ ███████╗",
		" ██║     ██║██╔══██╗██╔══██╗██╔════╝██╔════╝██╔═══██╗██╔══██╗██╔════╝",
		" ██║     ██║██████╔╝██████╔╝█████╗  ██║     ██║   ██║██║  ██║█████╗  ",
		" ██║     ██║██╔══██╗██╔══██╗██╔══╝  ██║     ██║   ██║██║  ██║██╔══╝  ",
		" ███████╗██║██████╔╝██║  ██║███████╗╚██████╗╚██████╔╝██████╔╝███████╗",
		" ╚══════╝╚═╝╚═════╝ ╚═╝  ╚═╝╚══════╝ ╚═════╝ ╚═════╝ ╚═════╝ ╚══════╝",
	}
}

func (app *App) addWelcomeMessage() {
	app.addSystemMessage(welcomeMessagePrefix + strings.Join(welcomeBodyLines(app.cwd), "\n"))
}

func (app *App) renderWelcomeMessage(width int, content string) []rendertext.Line {
	bodyLines := welcomeLinesFromContent(content)
	lines := make([]rendertext.Line, 0, len(bodyLines)+(welcomePaddingY*welcomeDoublePadding))
	app.appendWelcomePaddingLines(&lines, width, welcomePaddingY)

	for index, line := range bodyLines {
		lines = append(lines, app.welcomeStyledLine(width, index, line))
	}

	app.appendWelcomePaddingLines(&lines, width, welcomePaddingY)

	return lines
}

func (app *App) drawWelcomeOnly(width, height, row int) int {
	bodyLines := welcomeBodyLines(app.cwd)
	availableRows := max(1, height-row-app.composerReserve(width, height))
	marginRows := min(welcomeTopMarginRows, max(0, availableRows-1))
	row += marginRows
	availableRows -= marginRows

	bodyRows := min(len(bodyLines), max(0, availableRows-(welcomePaddingY*welcomeDoublePadding)))
	if bodyRows == 0 && availableRows > 0 {
		bodyRows = min(len(bodyLines), availableRows)
	}

	if welcomePaddingY > 0 && availableRows > bodyRows {
		paddingRows := min(welcomePaddingY, availableRows-bodyRows)
		app.writeWelcomePaddingRows(row, width, paddingRows)
		row += paddingRows
		availableRows -= paddingRows
	}

	bodyLines = bodyLines[:bodyRows]
	for index, line := range bodyLines {
		app.writeWelcomeLine(row, width, index, line)
		row++
	}

	remainingRows := max(0, availableRows-bodyRows)
	app.writeWelcomePaddingRows(row, width, min(welcomePaddingY, remainingRows))

	return row + min(welcomePaddingY, remainingRows)
}

func (app *App) writeWelcomeLine(row, width, lineIndex int, content string) {
	line := app.welcomeStyledLine(width, lineIndex, content)
	writeLine(app.frame, row, width, line.Text, line.Style)
}

func (app *App) welcomeStyledLine(width, lineIndex int, content string) rendertext.Line {
	style := app.welcomeBodyStyle(lineIndex, content)
	innerWidth := max(1, width-(welcomePaddingX*welcomeDoublePadding))
	centeredContent := centerText(rendertext.Truncate(content, innerWidth), innerWidth)
	paddedContent := strings.Repeat(" ", welcomePaddingX) +
		centeredContent +
		strings.Repeat(" ", welcomePaddingX)

	return rendertext.NewLine(style, rendertext.Truncate(paddedContent, width))
}

func centerText(text string, width int) string {
	if width <= 0 {
		return ""
	}

	text = rendertext.Truncate(text, width)
	padding := max(0, width-rendertext.Width(text))
	leftPadding := padding / welcomeDoublePadding
	rightPadding := padding - leftPadding

	return strings.Repeat(" ", leftPadding) + text + strings.Repeat(" ", rightPadding)
}

func (app *App) appendWelcomePaddingLines(lines *[]rendertext.Line, width, count int) {
	style := app.theme.background(colorCustomMessageBg)
	for range count {
		*lines = append(*lines, rendertext.NewLine(style, rendertext.PadRight("", width)))
	}
}

func (app *App) writeWelcomePaddingRows(row, width, count int) {
	style := app.theme.background(colorCustomMessageBg)
	for offset := range count {
		writeLine(app.frame, row+offset, width, rendertext.PadRight("", width), style)
	}
}

func (app *App) showWelcomeOnly() bool {
	return app.mode == modeChat &&
		len(app.transcript.History) == 1 &&
		isWelcomeMessage(app.transcript.History[0].Content)
}

func isWelcomeMessage(content string) bool {
	return strings.HasPrefix(content, welcomeMessagePrefix)
}

func (app *App) welcomeBodyStyle(index int, line string) tcell.Style {
	if index < len(welcomeArt()) {
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
	art := welcomeArt()
	lines := make([]string, 0, len(art)+welcomeArtExtraLines)
	lines = append(lines, art...)
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
