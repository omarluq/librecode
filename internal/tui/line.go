package tui

import "github.com/gdamore/tcell/v3"

type styledSegment struct {
	Style tcell.Style
	Text  string
	Width int
}

// Width returns the terminal display width of the line.
func (line Line) Width() int {
	if len(line.Spans) == 0 {
		return Width(line.Text)
	}

	width := 0
	for _, span := range line.Spans {
		width += Width(span.Text)
	}

	return width
}

// Clone returns a copy of the line and its spans.
func (line Line) Clone() Line {
	line.Spans = append([]Span{}, line.Spans...)

	return line
}

// Truncate returns a copy of the line clipped to width cells.
func (line Line) Truncate(width int) Line {
	if width <= 0 {
		return NewLine(line.Style, "")
	}

	if line.Width() <= width {
		return line.Clone()
	}

	if len(line.Spans) == 0 {
		return NewLine(line.Style, Truncate(line.Text, width))
	}

	ellipsisWidth := Width("…")
	if width == ellipsisWidth {
		return lineFromStyledSegments([]styledSegment{{Text: "…", Width: ellipsisWidth, Style: line.Style}}, line.Style)
	}

	segments := line.styledSegments()
	prefix := styledPrefix(segments, width-ellipsisWidth)
	prefix = append(prefix, styledSegment{Text: "…", Width: ellipsisWidth, Style: lastSegmentStyle(prefix, line.Style)})

	return lineFromStyledSegments(prefix, line.Style)
}

// Wrap returns display lines wrapped to width cells while preserving span styles.
func (line Line) Wrap(width int) []Line {
	return line.wrapWithMode(width, false)
}

// WrapPreserveWhitespace returns display lines wrapped to width cells without trimming wrapping whitespace.
func (line Line) WrapPreserveWhitespace(width int) []Line {
	return line.wrapWithMode(width, true)
}

// WrapCells returns display lines hard-wrapped to width cells while preserving span styles.
func (line Line) WrapCells(width int) []Line {
	return wrapStyledSegmentsCells(line.styledSegments(), line.Style, width)
}

func wrapStyledSegmentsCells(segments []styledSegment, style tcell.Style, width int) []Line {
	if width <= 0 {
		return []Line{NewLine(style, "")}
	}

	if len(segments) == 0 {
		return []Line{NewLine(style, "")}
	}

	lines := make([]Line, 0, 1)
	current := make([]styledSegment, 0, min(len(segments), width))
	used := 0

	for _, segment := range segments {
		if segment.Width > 0 && used > 0 && used+segment.Width > width {
			lines = append(lines, lineFromStyledSegments(current, style))
			current = current[:0]
			used = 0
		}

		current = append(current, segment)
		used += segment.Width

		if used >= width {
			lines = append(lines, lineFromStyledSegments(current, style))
			current = current[:0]
			used = 0
		}
	}

	if len(current) > 0 {
		lines = append(lines, lineFromStyledSegments(current, style))
	}

	if len(lines) == 0 {
		return []Line{NewLine(style, "")}
	}

	return lines
}

func (line Line) wrapWithMode(width int, preserveWhitespace bool) []Line {
	if width <= 0 {
		return []Line{NewLine(line.Style, "")}
	}

	if len(line.Spans) == 0 {
		return line.wrapPlainText(width, preserveWhitespace)
	}

	segments := line.styledSegments()
	lines := make([]Line, 0, 1)

	for len(segments) > 0 {
		breakIndex := styledWrapBreakIndex(segments, width)

		wrapped := segments[:breakIndex]
		if !preserveWhitespace {
			wrapped = trimTrailingSpaceSegments(wrapped)
		}

		lines = append(lines, lineFromStyledSegments(wrapped, line.Style))

		segments = segments[breakIndex:]
		if !preserveWhitespace {
			segments = trimLeadingSpaceSegments(segments)
		}
	}

	if len(lines) == 0 {
		return []Line{NewLine(line.Style, "")}
	}

	return lines
}

func (line Line) wrapPlainText(width int, preserveWhitespace bool) []Line {
	wrapped := Wrap(line.Text, width)
	if preserveWhitespace {
		wrapped = WrapPreserveWhitespace(line.Text, width)
	}

	lines := make([]Line, 0, len(wrapped))
	for _, text := range wrapped {
		lines = append(lines, NewLine(line.Style, text))
	}

	return lines
}

func (line Line) styledSegments() []styledSegment {
	spans := line.Spans
	if len(spans) == 0 {
		spans = []Span{{Text: line.Text, Style: line.Style}}
	}

	segments := make([]styledSegment, 0, len(spans))
	for _, span := range spans {
		for _, segment := range Segments(span.Text) {
			segments = append(segments, styledSegment{
				Text:  segment.Text,
				Width: segment.Width,
				Style: span.Style,
			})
		}
	}

	return segments
}

func styledPrefix(segments []styledSegment, width int) []styledSegment {
	prefix := make([]styledSegment, 0, len(segments))
	used := 0

	for _, segment := range segments {
		if segment.Width == 0 {
			prefix = append(prefix, segment)

			continue
		}

		if used+segment.Width > width {
			break
		}

		prefix = append(prefix, segment)
		used += segment.Width
	}

	return prefix
}

func styledWrapBreakIndex(segments []styledSegment, width int) int {
	return wrapBreakIndex(len(segments), width, func(index int) (string, int) {
		segment := segments[index]

		return segment.Text, segment.Width
	})
}

func lineFromStyledSegments(segments []styledSegment, fallback tcell.Style) Line {
	line := Line{Text: "", Style: fallback, Spans: []Span{}}
	for _, segment := range segments {
		line.Text += segment.Text
		if len(line.Spans) > 0 && line.Spans[len(line.Spans)-1].Style == segment.Style {
			line.Spans[len(line.Spans)-1].Text += segment.Text

			continue
		}

		line.Spans = append(line.Spans, Span{Text: segment.Text, Style: segment.Style})
	}

	if len(line.Spans) == 0 {
		line.Spans = nil
	}

	return line
}

func lastSegmentStyle(segments []styledSegment, fallback tcell.Style) tcell.Style {
	if len(segments) == 0 {
		return fallback
	}

	return segments[len(segments)-1].Style
}

func trimLeadingSpaceSegments(segments []styledSegment) []styledSegment {
	for len(segments) > 0 && (segments[0].Text == " " || segments[0].Text == "\t") {
		segments = segments[1:]
	}

	return segments
}

func trimTrailingSpaceSegments(segments []styledSegment) []styledSegment {
	for len(segments) > 0 && (segments[len(segments)-1].Text == " " || segments[len(segments)-1].Text == "	") {
		segments = segments[:len(segments)-1]
	}

	return segments
}
