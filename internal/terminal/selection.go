package terminal

import (
	"strings"

	"github.com/gdamore/tcell/v3"
)

type mouseSelection struct {
	startX int
	startY int
	endX   int
	endY   int
	active bool
}

func emptyMouseSelection() mouseSelection {
	return mouseSelection{
		startX: 0,
		startY: 0,
		endX:   0,
		endY:   0,
		active: false,
	}
}

func (selection mouseSelection) empty() bool {
	return !selection.active && selection.startX == selection.endX && selection.startY == selection.endY
}

func (selection mouseSelection) normalized() (startX, startY, endX, endY int) {
	startX, startY = selection.startX, selection.startY
	endX, endY = selection.endX, selection.endY
	if startY > endY || (startY == endY && startX > endX) {
		startX, endX = endX, startX
		startY, endY = endY, startY
	}

	return startX, startY, endX, endY
}

func (selection mouseSelection) contains(column, row int) bool {
	if selection.empty() {
		return false
	}
	startX, startY, endX, endY := selection.normalized()
	if row < startY || row > endY {
		return false
	}
	if startY == endY {
		return column >= startX && column < endX
	}
	if row == startY {
		return column >= startX
	}
	if row == endY {
		return column < endX
	}

	return true
}

func (selection mouseSelection) textFrom(frame *cellBuffer) string {
	if frame == nil || selection.empty() {
		return ""
	}
	startX, startY, endX, endY := selection.normalized()
	startY = max(0, min(startY, frame.height-1))
	endY = max(0, min(endY, frame.height-1))
	if startY > endY {
		return ""
	}

	lines := make([]string, 0, endY-startY+1)
	for row := startY; row <= endY; row++ {
		from, to := selectionRowBounds(row, startX, startY, endX, endY, frame.width)
		if from >= to {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, selectedFrameLine(frame, row, from, to))
	}

	return strings.Join(lines, "\n")
}

func selectionRowBounds(row, startX, startY, endX, endY, width int) (from, to int) {
	from = 0
	to = width
	if row == startY {
		from = startX
	}
	if row == endY {
		to = endX
	}
	from = max(0, min(from, width))
	to = max(0, min(to, width))

	return from, to
}

func selectedFrameLine(frame *cellBuffer, row, from, to int) string {
	var builder strings.Builder
	for column := from; column < to; column++ {
		builder.WriteRune(frame.cell(column, row).Rune)
	}

	return strings.TrimRight(builder.String(), " ")
}

func (app *App) beginMouseSelection(column, row int) {
	app.selection = mouseSelection{
		startX: column,
		startY: row,
		endX:   column,
		endY:   row,
		active: true,
	}
}

func (app *App) updateMouseSelection(column, row int) {
	if !app.selection.active {
		return
	}
	app.selection.endX = column
	app.selection.endY = row
}

func (app *App) finishMouseSelection(column, row int) {
	if !app.selection.active {
		return
	}
	app.updateMouseSelection(column, row)
	app.selection.active = false
	app.copySelectionToClipboard()
}

func (app *App) copySelectionToClipboard() {
	if app.screen == nil || app.frame == nil {
		return
	}
	text := app.selection.textFrom(app.frame)
	if text == "" {
		return
	}
	copyTextToClipboard(app.screen, text)
}

func copyTextToClipboard(screen tcell.Screen, text string) {
	if screen == nil || text == "" {
		return
	}
	screen.SetClipboard([]byte(text))
	if err := writeSystemClipboard(text); err != nil {
		return
	}
}

func (app *App) applySelectionHighlight() {
	if app.frame == nil || app.selection.empty() {
		return
	}
	for row := range app.frame.height {
		for column := range app.frame.width {
			if !app.selection.contains(column, row) {
				continue
			}
			cell := app.frame.cell(column, row)
			cell.Style = app.selectionStyle(cell.Style)
			app.frame.SetContent(column, row, cell.Rune, nil, cell.Style)
		}
	}
}

func (app *App) selectionStyle(style tcell.Style) tcell.Style {
	return style.Background(app.theme.colors[colorSelectedBg]).Foreground(app.theme.colors[colorAccent]).Bold(true)
}
