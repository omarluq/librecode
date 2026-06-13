package tui_test

import (
	"strings"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/tui"
)

const (
	testAlpha = "alpha"
	testBeta  = "beta"
	testHello = "hello"
	testOne   = "one"
)

func testRect(x, y, width, height int) tui.Rect {
	return tui.Rect{X: x, Y: y, Width: width, Height: height}
}

func testListItem(value, title, description, meta string) tui.ListItem {
	return tui.ListItem{Value: value, Title: title, Description: description, Meta: meta}
}

func testTableCell(text string) tui.TableCell {
	return tui.TableCell{Style: tcell.StyleDefault, Text: text}
}

func testTreeNode(text string, expanded, selected bool, children ...*tui.TreeNode) *tui.TreeNode {
	return &tui.TreeNode{
		Style:    tcell.StyleDefault,
		Value:    text,
		Text:     text,
		Children: children,
		Expanded: expanded,
		Selected: selected,
	}
}

func testLine(text string) tui.Line {
	return tui.Line{Style: tcell.StyleDefault, Text: text, Spans: nil}
}

func listOptions(width, height int, hints tui.ListHints) *tui.ListRenderOptions {
	return &tui.ListRenderOptions{Styles: emptyListStyles(), Hints: hints, Width: width, Height: height}
}

func emptyListHints() tui.ListHints {
	return tui.ListHints{Up: "", Down: "", Confirm: "", Cancel: ""}
}

func emptyListStyles() tui.ListStyles {
	return tui.ListStyles{
		Border:   tcell.StyleDefault,
		Accent:   tcell.StyleDefault,
		Muted:    tcell.StyleDefault,
		Text:     tcell.StyleDefault,
		Selected: tcell.StyleDefault,
		Dim:      tcell.StyleDefault,
	}
}

func bufferLine(buffer *tui.CellBuffer, row int) string {
	var builder strings.Builder
	for column := range buffer.Width() {
		builder.WriteRune(buffer.Cell(column, row).Rune)
	}

	return builder.String()
}

func lineTexts(lines []tui.Line) []string {
	texts := make([]string, 0, len(lines))
	for _, line := range lines {
		texts = append(texts, line.Text)
	}

	return texts
}

type rectRecorder struct {
	rects []tui.Rect
}

func (recorder *rectRecorder) Draw(_ tui.ContentSetter, rect tui.Rect) {
	recorder.rects = append(recorder.rects, rect)
}

type cellRecordingScreen struct {
	calls []cellWrite
}

type cellWrite struct {
	combining []rune
	primary   rune
}

func (screen *cellRecordingScreen) SetContent(_, _ int, primary rune, combining []rune, _ tcell.Style) {
	screen.calls = append(screen.calls, cellWrite{primary: primary, combining: append([]rune(nil), combining...)})
}

type recordingScreen struct {
	cells map[[2]int]rune
}

func (screen *recordingScreen) SetContent(x, y int, primary rune, _ []rune, _ tcell.Style) {
	screen.cells[[2]int{x, y}] = primary
}
