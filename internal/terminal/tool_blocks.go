package terminal

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v3"
)

const (
	maxCollapsedToolOutputLines = 5
	toolSectionTool             = "tool"
	toolSectionArguments        = "arguments"
	toolSectionDetails          = "details"
	toolSectionError            = "error"
	toolSectionOutput           = "output"
)

type parsedToolEvent struct {
	Name          string
	ArgumentsJSON string
	DetailsJSON   string
	Error         string
	Output        string
}

func (app *App) renderToolBlock(width int, message chatMessage) []styledLine {
	event := parseToolEventContent(message.Content, string(message.Role))
	style := app.toolBlockStyle(&event)
	if !app.toolsExpanded {
		return app.renderCollapsedToolBlock(width, &event, style)
	}

	return app.renderExpandedToolBlock(width, &event, style)
}

func (app *App) toolBlockStyle(event *parsedToolEvent) tcell.Style {
	if event.Error != "" {
		return app.theme.background(colorToolErrorBg)
	}

	return app.theme.background(colorToolSuccessBg)
}

func (app *App) renderCollapsedToolBlock(width int, event *parsedToolEvent, style tcell.Style) []styledLine {
	label := fmt.Sprintf("%s  %s expand", toolTitle(event), app.keys.hint(actionToolsExpand))
	lines := []styledLine{
		newStyledLine(tcell.StyleDefault, ""),
		newStyledLine(style.Bold(true), padRight(label, width)),
	}
	preview, hiddenLines := tailIndentedLines(width, toolEventOutput(event), style, maxCollapsedToolOutputLines)
	if hiddenLines > 0 {
		lines = append(lines, newStyledLine(
			style.Foreground(app.theme.colors[colorMuted]),
			padRight(app.hiddenToolLinesText(hiddenLines), width),
		))
	}
	lines = append(lines, preview...)
	lines = append(lines, newStyledLine(tcell.StyleDefault, ""))

	return lines
}

func (app *App) renderExpandedToolBlock(width int, event *parsedToolEvent, style tcell.Style) []styledLine {
	lines := make([]styledLine, 0, 10)
	label := fmt.Sprintf("%s  %s collapse", toolTitle(event), app.keys.hint(actionToolsExpand))
	lines = append(lines,
		newStyledLine(tcell.StyleDefault, ""),
		newStyledLine(style.Bold(true), padRight(label, width)),
	)
	lines = append(lines, app.toolArgumentLines(width, event, style)...)
	lines = append(lines, app.toolDiffLines(width, event, style)...)
	lines = append(lines, app.toolOutputLines(width, event, style)...)
	lines = append(lines, newStyledLine(tcell.StyleDefault, ""))

	return lines
}

func (app *App) toolArgumentLines(width int, event *parsedToolEvent, style tcell.Style) []styledLine {
	arguments := compactJSON(event.ArgumentsJSON)
	if arguments == "" {
		return nil
	}

	return plainSectionLines(width, "args", arguments, style)
}

func (app *App) toolDiffLines(width int, event *parsedToolEvent, style tcell.Style) []styledLine {
	diff := diffFromToolDetails(event.DetailsJSON)
	if diff == "" {
		return nil
	}
	innerWidth := max(1, width-2)
	baseStyle := app.theme.background(colorCodeBg).Foreground(app.theme.colors[colorCodeText])
	content := diffStyledLines(diff, app.theme, baseStyle)
	lines := []styledLine{newStyledLine(style.Bold(true), padRight("diff:", width))}
	for _, line := range content {
		for _, wrapped := range wrapTextPreserveWhitespace(line.Text, innerWidth) {
			lines = append(lines, newStyledLine(line.Style, padRight("  "+wrapped, width)))
		}
	}

	return lines
}

func (app *App) toolOutputLines(width int, event *parsedToolEvent, style tcell.Style) []styledLine {
	output := toolEventOutput(event)
	if output == "" {
		return nil
	}

	return plainSectionLines(width, "output", output, style)
}

func plainSectionLines(width int, label, content string, style tcell.Style) []styledLine {
	contentLines := indentedLines(width, content, style)
	lines := make([]styledLine, 0, len(contentLines)+1)
	lines = append(lines, newStyledLine(style.Bold(true), padRight(label+":", width)))

	return append(lines, contentLines...)
}

func indentedLines(width int, content string, style tcell.Style) []styledLine {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	innerWidth := max(1, width-2)
	lines := []styledLine{}
	for line := range strings.SplitSeq(content, "\n") {
		for _, wrapped := range wrapTextPreserveWhitespace(line, innerWidth) {
			lines = append(lines, newStyledLine(style, padRight("  "+wrapped, width)))
		}
	}

	return lines
}

func tailIndentedLines(width int, content string, style tcell.Style, limit int) (tail []styledLine, hidden int) {
	lines := indentedLines(width, content, style)
	if limit <= 0 || len(lines) <= limit {
		return lines, 0
	}
	hiddenLines := len(lines) - limit

	return lines[hiddenLines:], hiddenLines
}

func (app *App) renderToolMessageTail(width int, message chatMessage, rowsNeeded int) ([]styledLine, bool) {
	if rowsNeeded <= 0 {
		return nil, true
	}
	event := parseToolEventContent(message.Content, string(message.Role))
	style := app.toolBlockStyle(&event)
	footer := newStyledLine(tcell.StyleDefault, "")
	tailBudget := max(0, rowsNeeded-1)
	if tailBudget == 0 {
		return []styledLine{footer}, false
	}
	tail, hidden := tailExpandedToolLines(width, &event, style, tailBudget)
	if hidden {
		return append(tail, footer), false
	}
	prefix := app.expandedToolPrefixLines(width, &event, style)
	lines := make([]styledLine, 0, len(prefix)+len(tail)+1)
	lines = append(lines, prefix...)
	lines = append(lines, tail...)
	lines = append(lines, footer)
	if len(lines) <= rowsNeeded {
		return lines, true
	}

	return lines[len(lines)-rowsNeeded:], false
}

func (app *App) expandedToolPrefixLines(width int, event *parsedToolEvent, style tcell.Style) []styledLine {
	label := fmt.Sprintf("%s  %s collapse", toolTitle(event), app.keys.hint(actionToolsExpand))
	lines := make([]styledLine, 0, 2)
	lines = append(lines,
		newStyledLine(tcell.StyleDefault, ""),
		newStyledLine(style.Bold(true), padRight(label, width)),
	)
	lines = append(lines, app.toolArgumentLines(width, event, style)...)
	lines = append(lines, app.toolDiffLines(width, event, style)...)

	return lines
}

func tailExpandedToolLines(width int, event *parsedToolEvent, style tcell.Style, rowsNeeded int) ([]styledLine, bool) {
	if event.Output != "" {
		return tailSectionLinesFromEnd(width, "output", event.Output, style, rowsNeeded)
	}
	if event.Error != "" {
		return tailSectionLinesFromEnd(width, "error", event.Error, style, rowsNeeded)
	}
	if event.DetailsJSON != "" {
		return tailSectionLinesFromEnd(width, "details", compactJSON(event.DetailsJSON), style, rowsNeeded)
	}
	if event.ArgumentsJSON != "" {
		return tailSectionLinesFromEnd(width, "args", compactJSON(event.ArgumentsJSON), style, rowsNeeded)
	}

	return nil, false
}

func tailSectionLinesFromEnd(width int, label, content string, style tcell.Style, rowsNeeded int) ([]styledLine, bool) {
	if rowsNeeded <= 0 || strings.TrimSpace(content) == "" {
		return nil, false
	}
	if rowsNeeded == 1 {
		return []styledLine{newStyledLine(style.Bold(true), padRight(label+":", width))}, true
	}
	tail, hidden := tailIndentedLinesFromEnd(width, content, style, rowsNeeded-1)
	lines := make([]styledLine, 0, len(tail)+1)
	lines = append(lines, newStyledLine(style.Bold(true), padRight(label+":", width)))
	lines = append(lines, tail...)

	return lines, hidden
}

func tailIndentedLinesFromEnd(width int, content string, style tcell.Style, limit int) ([]styledLine, bool) {
	if limit <= 0 || strings.TrimSpace(content) == "" {
		return nil, false
	}
	collector := tailLineCollector{
		Style:  style,
		Lines:  make([]styledLine, 0, limit),
		Width:  width,
		Limit:  limit,
		Hidden: false,
	}
	collector.Collect(strings.Trim(content, "\n"))
	reverseStyledLines(collector.Lines)

	return collector.Lines, collector.Hidden
}

type tailLineCollector struct {
	Style  tcell.Style
	Lines  []styledLine
	Width  int
	Limit  int
	Hidden bool
}

func (collector *tailLineCollector) Collect(content string) {
	for end := len(content); end >= 0 && len(collector.Lines) < collector.Limit; {
		start := strings.LastIndexByte(content[:end], '\n') + 1
		collector.collectWrappedLine(content[start:end])
		if start == 0 {
			return
		}
		end = start - 1
	}
	collector.Hidden = true
}

func (collector *tailLineCollector) collectWrappedLine(line string) {
	wrapped := wrapTextPreserveWhitespace(line, max(1, collector.Width-2))
	added := 0
	for index := len(wrapped) - 1; index >= 0 && len(collector.Lines) < collector.Limit; index-- {
		collector.Lines = append(collector.Lines, newStyledLine(
			collector.Style,
			padRight("  "+wrapped[index], collector.Width),
		))
		added++
	}
	if added < len(wrapped) {
		collector.Hidden = true
	}
}

func reverseStyledLines(lines []styledLine) {
	for left, right := 0, len(lines)-1; left < right; left, right = left+1, right-1 {
		lines[left], lines[right] = lines[right], lines[left]
	}
}

func (app *App) hiddenToolLinesText(hiddenLines int) string {
	unit := "lines"
	if hiddenLines == 1 {
		unit = "line"
	}

	return "  … " + intText(hiddenLines) + " earlier output " + unit + " hidden; " +
		app.keys.hint(actionToolsExpand) + " expand"
}

func toolEventOutput(event *parsedToolEvent) string {
	output := strings.Trim(event.Output, "\n")
	if event.Error != "" {
		output = strings.Trim(event.Error+"\n"+output, "\n")
	}

	return output
}

func parseToolEventContent(content, fallback string) parsedToolEvent {
	event := parsedToolEvent{
		Name:          fallback,
		ArgumentsJSON: "",
		DetailsJSON:   "",
		Error:         "",
		Output:        "",
	}
	current := ""
	sections := map[string][]string{}
	for line := range strings.SplitSeq(content, "\n") {
		if name, value, ok := parseToolSectionHeader(line); ok {
			if name == toolSectionTool {
				event.Name = value
				current = ""
				continue
			}
			current = name
			if value != "" {
				sections[current] = append(sections[current], value)
			}
			continue
		}
		if current != "" {
			sections[current] = append(sections[current], line)
		}
	}
	event.ArgumentsJSON = strings.TrimSpace(strings.Join(sections[toolSectionArguments], "\n"))
	event.DetailsJSON = strings.TrimSpace(strings.Join(sections[toolSectionDetails], "\n"))
	event.Error = strings.Trim(strings.Join(sections[toolSectionError], "\n"), "\n")
	event.Output = strings.Trim(strings.Join(sections[toolSectionOutput], "\n"), "\n")

	return event
}

func parseToolSectionHeader(line string) (name, value string, ok bool) {
	left, right, found := strings.Cut(line, ":")
	if !found {
		return "", "", false
	}
	name = strings.TrimSpace(left)
	switch name {
	case toolSectionTool, toolSectionArguments, toolSectionDetails, toolSectionError, toolSectionOutput:
		return name, strings.TrimSpace(right), true
	default:
		return "", "", false
	}
}

func toolTitle(event *parsedToolEvent) string {
	name := strings.TrimSpace(event.Name)
	if name == "" {
		name = toolSectionTool
	}
	if event.Error != "" {
		return "✗ " + name
	}
	if after, ok := strings.CutPrefix(name, "load skill: "); ok {
		return "loaded skill " + strings.TrimSpace(after)
	}

	return "✓ " + name
}

func diffFromToolDetails(detailsJSON string) string {
	if strings.TrimSpace(detailsJSON) == "" {
		return ""
	}
	var details map[string]any
	if err := json.Unmarshal([]byte(detailsJSON), &details); err != nil {
		return ""
	}
	diff, ok := details["diff"].(string)
	if !ok {
		return ""
	}

	return strings.TrimSpace(diff)
}
