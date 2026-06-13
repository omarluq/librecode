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

// RoundedBorder is the default rounded border style.
var RoundedBorder = Border{
	Horizontal:  borderHorizontal,
	Vertical:    borderVertical,
	TopLeft:     borderTopLeft,
	TopRight:    borderTopRight,
	BottomLeft:  borderBottomLeft,
	BottomRight: borderBottomRight,
	MiddleLeft:  borderMiddleLeft,
	MiddleRight: borderMiddleRight,
}

// TopBorder returns a rounded top border with an optional title.
func TopBorder(width int, title string) string {
	return borderLineWithBorder(width, title, RoundedBorder.TopLeft, RoundedBorder.TopRight, RoundedBorder)
}

// MiddleBorder returns a rounded separator border.
func MiddleBorder(width int) string {
	return borderLineWithBorder(width, "", RoundedBorder.MiddleLeft, RoundedBorder.MiddleRight, RoundedBorder)
}

// BottomBorder returns a rounded bottom border.
func BottomBorder(width int) string {
	return borderLineWithBorder(width, "", RoundedBorder.BottomLeft, RoundedBorder.BottomRight, RoundedBorder)
}

// Box draws a border and optional title around a rectangle.
type Box struct {
	Title  string
	Style  tcell.Style
	Border Border
}

// NewBox returns a Box using the rounded border.
func NewBox(title string) *Box {
	return &Box{Title: title, Border: RoundedBorder}
}

// Draw draws the box border into rect.
func (box *Box) Draw(screen Screen, rect Rect) {
	if screen == nil || rect.Empty() || box == nil {
		return
	}

	border := box.Border
	if !border.complete() {
		border = RoundedBorder
	}

	if rect.Height >= 1 {
		DrawText(screen, Rect{X: rect.X, Y: rect.Y, Width: rect.Width, Height: 1}, box.Style, borderLineWithBorder(rect.Width, box.Title, border.TopLeft, border.TopRight, border))
	}
	for row := 1; row < rect.Height-1; row++ {
		DrawText(screen, Rect{X: rect.X, Y: rect.Y + row, Width: 1, Height: 1}, box.Style, border.Vertical)
		DrawText(screen, Rect{X: rect.X + rect.Width - 1, Y: rect.Y + row, Width: 1, Height: 1}, box.Style, border.Vertical)
	}
	if rect.Height >= 2 {
		DrawText(screen, Rect{X: rect.X, Y: rect.Y + rect.Height - 1, Width: rect.Width, Height: 1}, box.Style, borderLineWithBorder(rect.Width, "", border.BottomLeft, border.BottomRight, border))
	}
}

func (border Border) complete() bool {
	return border.Horizontal != "" &&
		border.Vertical != "" &&
		border.TopLeft != "" &&
		border.TopRight != "" &&
		border.BottomLeft != "" &&
		border.BottomRight != ""
}

func borderLineWithBorder(width int, title, left, right string, border Border) string {
	if width <= 0 {
		return ""
	}
	if width == 1 {
		return left
	}

	innerWidth := width - 2
	inner := strings.Repeat(border.Horizontal, innerWidth)
	if title != "" && innerWidth > 0 {
		label := " " + title + " "
		label = Truncate(label, innerWidth)
		inner = label + strings.Repeat(border.Horizontal, max(0, innerWidth-Width(label)))
	}

	return left + inner + right
}
