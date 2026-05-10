package terminal

import (
	"strings"

	"github.com/gdamore/tcell/v3"
)

func writeLine(screen cellTarget, row, width int, text string, style tcell.Style) {
	writeTextAt(screen, 0, row, width, text, style)
}

func writeLineWithVerticalBorders(
	screen cellTarget,
	row int,
	width int,
	text string,
	style tcell.Style,
	borderColor tcell.Color,
) {
	writeLineWithStyleFunc(screen, row, width, text, style, func(segment terminalTextSegment, used int) tcell.Style {
		if segment.Text == "│" && (used == 0 || used == width-1) {
			return style.Foreground(borderColor)
		}

		return style
	})
}

func writeTextAt(screen cellTarget, column, row, width int, text string, style tcell.Style) {
	writeTextCells(screen, column, row, width, text, style)
}

func writeShimmerLineWithVerticalBorders(
	screen cellTarget,
	row int,
	width int,
	text string,
	style tcell.Style,
	borderColor tcell.Color,
	frame int,
) {
	spinnerStart, spinnerWidth := workingSpinnerRange(text)
	contentStart, contentWidth := workingShimmerContentRange(text)
	writeLineWithStyleFunc(screen, row, width, text, style, func(segment terminalTextSegment, used int) tcell.Style {
		if segment.Text == "│" && (used == 0 || used == width-1) {
			return style.Foreground(borderColor)
		}
		if spinnerWidth > 0 && used >= spinnerStart && used < spinnerStart+spinnerWidth {
			return style.Foreground(workingShimmerBrightColor())
		}
		if contentWidth == 0 || used < contentStart || used >= contentStart+contentWidth {
			return style
		}

		return style.Foreground(workingShimmerColor(frame, used-contentStart, contentWidth))
	})
}

func isWorkingIndicatorText(text string) bool {
	return workingIndicatorParts(text).label != ""
}

func workingSpinnerRange(text string) (start, width int) {
	parts := workingIndicatorParts(text)
	if parts.label == "" {
		return 0, 0
	}

	return parts.spinnerStart, terminalTextWidth(parts.spinner)
}

func workingShimmerContentRange(text string) (start, width int) {
	parts := workingIndicatorParts(text)
	if parts.label == "" {
		return 0, 0
	}

	return parts.labelStart, terminalTextWidth(parts.label)
}

type workingIndicatorPartsResult struct {
	spinner      string
	label        string
	spinnerStart int
	labelStart   int
}

func emptyWorkingIndicatorParts() workingIndicatorPartsResult {
	return workingIndicatorPartsResult{
		spinner:      "",
		label:        "",
		spinnerStart: 0,
		labelStart:   0,
	}
}

func workingIndicatorParts(text string) workingIndicatorPartsResult {
	trimmedLeft := strings.TrimLeft(text, " ")
	spinner := firstField(trimmedLeft)
	if !isWorkingSpinner(spinner) {
		return emptyWorkingIndicatorParts()
	}
	afterSpinner := strings.TrimPrefix(trimmedLeft, spinner)
	label := strings.TrimLeft(strings.TrimRight(afterSpinner, " "), " ")
	if label == "" {
		return emptyWorkingIndicatorParts()
	}
	beforeLabel := text[:len(text)-len(afterSpinner)]
	labelPadding := terminalTextWidth(afterSpinner) - terminalTextWidth(strings.TrimLeft(afterSpinner, " "))

	return workingIndicatorPartsResult{
		spinner:      spinner,
		label:        label,
		spinnerStart: terminalTextWidth(text) - terminalTextWidth(trimmedLeft),
		labelStart:   terminalTextWidth(beforeLabel) + labelPadding,
	}
}

func firstField(text string) string {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return ""
	}

	return fields[0]
}

func isWorkingSpinner(text string) bool {
	switch text {
	case "⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏":
		return true
	default:
		return false
	}
}

func writeLineWithStyleFunc(
	screen cellTarget,
	row int,
	width int,
	text string,
	defaultStyle tcell.Style,
	styleFor func(segment terminalTextSegment, used int) tcell.Style,
) {
	if row < 0 || width <= 0 {
		return
	}
	used := 0
	for _, segment := range terminalTextSegments(text) {
		if used+segment.Width > width {
			break
		}
		used += writeTextSegment(screen, used, row, width-used, segment, styleFor(segment, used))
	}
	for used < width {
		screen.SetContent(used, row, ' ', nil, defaultStyle)
		used++
	}
}

func writeEditorLine(
	screen cellTarget,
	row int,
	width int,
	line styledLine,
	lineIndex int,
	lineCount int,
	borderStyle tcell.Style,
) {
	if lineIndex == 0 || lineIndex == lineCount-1 {
		writeLine(screen, row, width, line.Text, line.Style)
		return
	}
	if row < 0 {
		return
	}
	used := writeEditorLineText(screen, row, width, line, borderStyle)
	writeEditorLinePadding(screen, row, width, used, line, borderStyle)
}

func writeEditorLineText(
	screen cellTarget,
	row int,
	width int,
	line styledLine,
	borderStyle tcell.Style,
) int {
	used := 0
	for _, segment := range terminalTextSegments(line.Text) {
		if used+segment.Width > width {
			break
		}
		used += writeTextSegment(
			screen,
			used,
			row,
			width-used,
			segment,
			editorLineStyle(used, width, line, borderStyle),
		)
	}

	return used
}

func writeEditorLinePadding(
	screen cellTarget,
	row int,
	width int,
	used int,
	line styledLine,
	borderStyle tcell.Style,
) {
	for used < width {
		screen.SetContent(used, row, ' ', nil, editorLineStyle(used, width, line, borderStyle))
		used++
	}
}

func editorLineStyle(position, width int, line styledLine, borderStyle tcell.Style) tcell.Style {
	if position < 2 || position >= max(0, width-2) {
		return borderStyle
	}

	return line.Style
}
