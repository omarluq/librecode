package tui

import (
	"strings"

	"github.com/gdamore/tcell/v3"
)

const (
	borderHorizontal  = "─"
	borderVertical    = "│"
	borderTopLeft     = "╭"
	borderTopRight    = "╮"
	borderBottomLeft  = "╰"
	borderBottomRight = "╯"
	borderMiddleLeft  = "├"
	borderMiddleRight = "┤"
)

// Border contains the runes used to draw a box.
type Border struct {
	Horizontal  string
	Vertical    string
	TopLeft     string
	TopRight    string
	BottomLeft  string
	BottomRight string
	MiddleLeft  string
	MiddleRight string
}

const minBoxHeightWithBottom = 2

// RoundedBorder returns the default rounded border style.
func RoundedBorder() Border {
	return Border{
		Horizontal:  borderHorizontal,
		Vertical:    borderVertical,
		TopLeft:     borderTopLeft,
		TopRight:    borderTopRight,
		BottomLeft:  borderBottomLeft,
		BottomRight: borderBottomRight,
		MiddleLeft:  borderMiddleLeft,
		MiddleRight: borderMiddleRight,
	}
}

// TopBorder returns a rounded top border with an optional title.
func TopBorder(width int, title string) string {
	border := RoundedBorder()

	return borderLineWithBorder(width, title, border.TopLeft, border.TopRight, &border)
}

// MiddleBorder returns a rounded separator border.
func MiddleBorder(width int) string {
	border := RoundedBorder()

	return borderLineWithBorder(width, "", border.MiddleLeft, border.MiddleRight, &border)
}

// BottomBorder returns a rounded bottom border.
func BottomBorder(width int) string {
	border := RoundedBorder()

	return borderLineWithBorder(width, "", border.BottomLeft, border.BottomRight, &border)
}

// Box draws a border and optional title around a rectangle.
type Box struct {
	Title  string
	Style  tcell.Style
	Border Border
}

// NewBox returns a Box using the rounded border.
func NewBox(title string) *Box {
	return &Box{Title: title, Style: tcell.Style{}, Border: RoundedBorder()}
}

// Draw draws the box border into rect.
func (box *Box) Draw(screen ContentSetter, rect Rect) {
	if screen == nil || rect.Empty() || box == nil {
		return
	}

	border := box.Border
	if !border.complete() {
		border = RoundedBorder()
	}

	box.drawTop(screen, rect, &border)
	box.drawSides(screen, rect, &border)
	box.drawBottom(screen, rect, &border)
}

func (box *Box) drawTop(screen ContentSetter, rect Rect, border *Border) {
	if rect.Height < 1 {
		return
	}

	line := borderLineWithBorder(rect.Width, box.Title, border.TopLeft, border.TopRight, border)
	DrawText(screen, Rect{X: rect.X, Y: rect.Y, Width: rect.Width, Height: 1}, box.Style, line)
}

func (box *Box) drawSides(screen ContentSetter, rect Rect, border *Border) {
	for row := 1; row < rect.Height-1; row++ {
		leftRect := Rect{X: rect.X, Y: rect.Y + row, Width: 1, Height: 1}
		rightRect := Rect{X: rect.X + rect.Width - 1, Y: rect.Y + row, Width: 1, Height: 1}

		DrawText(screen, leftRect, box.Style, border.Vertical)
		DrawText(screen, rightRect, box.Style, border.Vertical)
	}
}

func (box *Box) drawBottom(screen ContentSetter, rect Rect, border *Border) {
	if rect.Height < minBoxHeightWithBottom {
		return
	}

	line := borderLineWithBorder(rect.Width, "", border.BottomLeft, border.BottomRight, border)
	bottomRect := Rect{X: rect.X, Y: rect.Y + rect.Height - 1, Width: rect.Width, Height: 1}
	DrawText(screen, bottomRect, box.Style, line)
}

func (border *Border) complete() bool {
	return border.Horizontal != "" &&
		border.Vertical != "" &&
		border.TopLeft != "" &&
		border.TopRight != "" &&
		border.BottomLeft != "" &&
		border.BottomRight != ""
}

func borderLineWithBorder(width int, title, left, right string, border *Border) string {
	if width <= 0 {
		return ""
	}

	if width == 1 {
		return left
	}

	innerWidth := width - len(left) - len(right)

	inner := strings.Repeat(border.Horizontal, innerWidth)
	if title != "" && innerWidth > 0 {
		label := " " + title + " "
		label = Truncate(label, innerWidth)
		inner = label + strings.Repeat(border.Horizontal, max(0, innerWidth-Width(label)))
	}

	return left + inner + right
}
