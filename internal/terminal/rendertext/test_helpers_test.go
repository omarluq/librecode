package rendertext_test

import (
	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/terminal/rendertext"
)

func testLine(text string) rendertext.Line {
	return rendertext.Line{Style: tcell.StyleDefault, Text: text, Spans: nil}
}
