// Package tui provides small reusable terminal UI primitives and components.
//
// It is intentionally lighter than tview: components render into tcell/v3
// screens, but most state is plain Go data that can also be tested by
// inspecting rendered lines.
package tui

import "github.com/gdamore/tcell/v3"

// Style aliases the tcell/v3 style type used throughout the package.
type Style = tcell.Style

// Color aliases the tcell/v3 color type used throughout the package.
type Color = tcell.Color

// ContentSetter is the subset of tcell.Screen required by draw helpers.
// It only requires SetContent so tests and buffers can provide lightweight sinks.
type ContentSetter interface {
	SetContent(column, row int, mainc rune, combc []rune, style tcell.Style)
}

// Drawer is the minimal drawable component contract.
type Drawer interface {
	Draw(screen ContentSetter, rect Rect)
}

// Rect describes a terminal rectangle.
type Rect struct {
	X      int
	Y      int
	Width  int
	Height int
}

// Empty reports whether rect has no drawable area.
func (rect Rect) Empty() bool {
	return rect.Width <= 0 || rect.Height <= 0
}

// Inner returns rect shrunk by padding on all sides.
func (rect Rect) Inner(padding int) Rect {
	if padding <= 0 {
		return rect
	}

	return Rect{
		X:      rect.X + padding,
		Y:      rect.Y + padding,
		Width:  max(0, rect.Width-padding*2),
		Height: max(0, rect.Height-padding*2),
	}
}

// Span is one styled segment inside a line.
type Span struct {
	Style tcell.Style
	Text  string
}

// Line is one terminal display line with optional per-span styles.
type Line struct {
	Text  string
	Style tcell.Style
	Spans []Span
}

// NewLine returns a line with one default style and no per-span overrides.
func NewLine(style tcell.Style, text string) Line {
	return Line{Text: text, Style: style, Spans: nil}
}
