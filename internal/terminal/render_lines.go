package terminal

import (
	"github.com/omarluq/librecode/internal/terminal/rendertext"
	"strings"

	"github.com/gdamore/tcell/v3"
)

func writeStyled(screen rendertext.ContentSetter, row, width int, line rendertext.Line) {
	if len(line.Spans) == 0 {
		writeLine(screen, row, width, line.Text, line.Style)

		return
	}

	used := 0
	for _, span := range line.Spans {
		if used >= width {
			break
		}

		used += rendertext.WriteCellsNoFill(screen, used, row, width-used, span.Text, span.Style)
	}

	for used < width {
		screen.SetContent(used, row, ' ', nil, line.Style)
		used++
	}
}

func writeLine(screen rendertext.ContentSetter, row, width int, text string, style tcell.Style) {
	writeTextAt(screen, 0, row, width, text, style)
}

func writeLineWithVerticalBorders(
	screen rendertext.ContentSetter,
	row int,
	width int,
	text string,
	style tcell.Style,
	borderColor tcell.Color,
) {
	writeLineWithStyleFunc(screen, row, width, text, style, func(segment rendertext.Segment, used int) tcell.Style {
		if segment.Text == "│" && (used == 0 || used == width-1) {
			return style.Foreground(borderColor)
		}

		return style
	})
}

func writeTextAt(screen rendertext.ContentSetter, column, row, width int, text string, style tcell.Style) {
	rendertext.WriteCells(screen, column, row, width, text, style)
}

type shimmerLineOptions struct {
	shimmerPosition int
	borderColor     tcell.Color
	palette         workingShimmerPalette
}

func writeShimmerLineWithVerticalBorders(
	screen rendertext.ContentSetter,
	row int,
	width int,
	text string,
	style tcell.Style,
	options shimmerLineOptions,
) {
	spinnerStart, spinnerWidth := workingSpinnerRange(text)
	contentStart, contentWidth := workingShimmerContentRange(text)
	writeLineWithStyleFunc(
		screen,
		row,
		width,
		text,
		style,
		func(segment rendertext.Segment, used int) tcell.Style {
			if segment.Text == "│" && (used == 0 || used == width-1) {
				return style.Foreground(options.borderColor)
			}

			if spinnerWidth > 0 && used >= spinnerStart && used < spinnerStart+spinnerWidth {
				return style.Foreground(options.palette.bright)
			}

			if contentWidth == 0 || used < contentStart || used >= contentStart+contentWidth {
				return style
			}

			return style.Foreground(workingShimmerColor(
				options.shimmerPosition,
				used-contentStart,
				contentWidth,
				options.palette,
			))
		},
	)
}

func isWorkingIndicatorText(text string) bool {
	return workingIndicatorParts(text).label != ""
}

func workingSpinnerRange(text string) (start, width int) {
	parts := workingIndicatorParts(text)
	if parts.label == "" {
		return 0, 0
	}

	return parts.spinnerStart, rendertext.Width(parts.spinner)
}

func workingShimmerContentRange(text string) (start, width int) {
	parts := workingIndicatorParts(text)
	if parts.label == "" {
		return 0, 0
	}

	return parts.labelStart, rendertext.Width(parts.label)
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
	labelPadding := rendertext.Width(afterSpinner) - rendertext.Width(strings.TrimLeft(afterSpinner, " "))

	return workingIndicatorPartsResult{
		spinner:      spinner,
		label:        label,
		spinnerStart: rendertext.Width(text) - rendertext.Width(trimmedLeft),
		labelStart:   rendertext.Width(beforeLabel) + labelPadding,
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
	screen rendertext.ContentSetter,
	row int,
	width int,
	text string,
	defaultStyle tcell.Style,
	styleFor func(segment rendertext.Segment, used int) tcell.Style,
) {
	if row < 0 || width <= 0 {
		return
	}

	used := 0
	for _, segment := range rendertext.Segments(text) {
		if used+segment.Width > width {
			break
		}

		used += rendertext.WriteSegment(screen, used, row, width-used, segment, styleFor(segment, used))
	}

	for used < width {
		screen.SetContent(used, row, ' ', nil, defaultStyle)
		used++
	}
}

func writeEditorLine(
	screen rendertext.ContentSetter,
	row int,
	width int,
	line rendertext.Line,
	lineIndex int,
	lineCount int,
	borderStyle tcell.Style,
) {
	if lineIndex == 0 || lineIndex == lineCount-1 {
		writeStyled(screen, row, width, line)

		return
	}

	if row < 0 {
		return
	}

	used := writeEditorLineText(screen, row, width, line, borderStyle)
	writeEditorLinePadding(screen, row, width, used, line, borderStyle)
}

func writeEditorLineText(
	screen rendertext.ContentSetter,
	row int,
	width int,
	line rendertext.Line,
	borderStyle tcell.Style,
) int {
	if len(line.Spans) == 0 {
		return writeEditorLinePlainText(screen, row, width, line, borderStyle)
	}

	return writeEditorLineSpans(screen, row, width, line, borderStyle)
}

func writeEditorLinePlainText(
	screen rendertext.ContentSetter,
	row int,
	width int,
	line rendertext.Line,
	borderStyle tcell.Style,
) int {
	used := 0
	for _, segment := range rendertext.Segments(line.Text) {
		if used+segment.Width > width {
			break
		}

		used += rendertext.WriteSegment(
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

func writeEditorLineSpans(
	screen rendertext.ContentSetter,
	row int,
	width int,
	line rendertext.Line,
	borderStyle tcell.Style,
) int {
	used := 0

	for _, span := range line.Spans {
		for _, segment := range rendertext.Segments(span.Text) {
			if used+segment.Width > width {
				return used
			}

			style := span.Style
			if used < terminalMarkerMargin || used >= max(0, width-terminalMarkerMargin) {
				style = borderStyle
			}

			used += rendertext.WriteSegment(screen, used, row, width-used, segment, style)
		}
	}

	return used
}

func writeEditorLinePadding(
	screen rendertext.ContentSetter,
	row int,
	width int,
	used int,
	line rendertext.Line,
	borderStyle tcell.Style,
) {
	for used < width {
		screen.SetContent(used, row, ' ', nil, editorLineStyle(used, width, line, borderStyle))
		used++
	}
}

func editorLineStyle(position, width int, line rendertext.Line, borderStyle tcell.Style) tcell.Style {
	if position < terminalMarkerMargin || position >= max(0, width-terminalMarkerMargin) {
		return borderStyle
	}

	return line.Style
}
