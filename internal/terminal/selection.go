package terminal

import (
	"strings"
	"time"

	"github.com/gdamore/tcell/v3"
)

const doubleClickDelay = 500 * time.Millisecond

type mouseSelection struct {
	lastClickUnixNano int64
	startX            int
	startY            int
	endX              int
	endY              int
	lastClickX        int
	lastClickY        int
	clickCount        int
	active            bool
}

func emptyMouseSelection() mouseSelection {
	return mouseSelection{
		lastClickUnixNano: 0,
		startX:            0,
		startY:            0,
		endX:              0,
		endY:              0,
		lastClickX:        0,
		lastClickY:        0,
		clickCount:        0,
		active:            false,
	}
}

func (selection *mouseSelection) empty() bool {
	return !selection.active && selection.startX == selection.endX && selection.startY == selection.endY
}

func (selection *mouseSelection) normalized() (startX, startY, endX, endY int) {
	startX, startY = selection.startX, selection.startY
	endX, endY = selection.endX, selection.endY
	if startY > endY || (startY == endY && startX > endX) {
		startX, endX = endX, startX
		startY, endY = endY, startY
	}

	return startX, startY, endX, endY
}

func (selection *mouseSelection) contains(column, row int) bool {
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

func (selection *mouseSelection) textFrom(frame *cellBuffer) string {
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

func selectedFrameLine(frame *cellBuffer, row, from, limit int) string {
	var builder strings.Builder
	for column := from; column < limit; column++ {
		builder.WriteRune(frame.cell(column, row).Rune)
	}
	text := builder.String()
	if limit >= frame.width {
		return strings.TrimRight(text, " ")
	}

	return text
}

func (app *App) beginMouseSelection(column, row int, clickedAt time.Time) {
	clickCount := app.nextMouseClickCount(column, row, clickedAt)
	switch {
	case clickCount%4 == 0:
		app.selectFrameLine(row, clickedAt, column, clickCount)
	case clickCount%2 == 0:
		app.selectFrameToken(column, row, clickedAt, clickCount)
	default:
		app.selection = mouseSelection{
			lastClickUnixNano: clickedAt.UnixNano(),
			startX:            column,
			startY:            row,
			endX:              column,
			endY:              row,
			lastClickX:        column,
			lastClickY:        row,
			clickCount:        clickCount,
			active:            true,
		}
	}
}

func (app *App) nextMouseClickCount(column, row int, clickedAt time.Time) int {
	if app.selection.lastClickUnixNano == 0 || row != app.selection.lastClickY {
		return 1
	}
	if intAbs(column-app.selection.lastClickX) > 1 {
		return 1
	}

	lastClickAt := time.Unix(0, app.selection.lastClickUnixNano)
	if clickedAt.Sub(lastClickAt) > doubleClickDelay {
		return 1
	}

	return app.selection.clickCount + 1
}

func intAbs(value int) int {
	if value < 0 {
		return -value
	}

	return value
}

func (app *App) selectFrameLine(row int, clickedAt time.Time, column, clickCount int) {
	width := 0
	if app.frame != nil {
		width = app.frame.width
	}
	app.selection = mouseSelection{
		lastClickUnixNano: clickedAt.UnixNano(),
		startX:            0,
		startY:            row,
		endX:              width,
		endY:              row,
		lastClickX:        column,
		lastClickY:        row,
		clickCount:        clickCount,
		active:            false,
	}
	app.copySelectionToClipboard()
}

func (app *App) selectFrameToken(column, row int, clickedAt time.Time, clickCount int) {
	start, end := app.frameTokenBounds(column, row)
	app.selection = mouseSelection{
		lastClickUnixNano: clickedAt.UnixNano(),
		startX:            start,
		startY:            row,
		endX:              end,
		endY:              row,
		lastClickX:        column,
		lastClickY:        row,
		clickCount:        clickCount,
		active:            false,
	}
	app.copySelectionToClipboard()
}

func (app *App) frameTokenBounds(column, row int) (start, end int) {
	if app.frame == nil || row < 0 || row >= app.frame.height {
		return column, column
	}
	column = max(0, min(column, app.frame.width-1))
	if app.frame.cell(column, row).Rune == ' ' {
		return app.frameWhitespaceBounds(column, row)
	}

	start = column
	for start > 0 && app.frame.cell(start-1, row).Rune != ' ' {
		start--
	}
	end = column + 1
	for end < app.frame.width && app.frame.cell(end, row).Rune != ' ' {
		end++
	}

	return start, end
}

func (app *App) frameWhitespaceBounds(column, row int) (start, end int) {
	start = column
	for start > 0 && app.frame.cell(start-1, row).Rune == ' ' {
		start--
	}
	end = column + 1
	for end < app.frame.width && app.frame.cell(end, row).Rune == ' ' {
		end++
	}

	return start, end
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
