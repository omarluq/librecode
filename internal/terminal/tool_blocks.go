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
		{Style: tcell.StyleDefault, Text: ""},
		{Style: style.Bold(true), Text: padRight(label, width)},
	}
	preview, truncated := tailIndentedLines(width, toolEventOutput(event), style, maxCollapsedToolOutputLines)
	if truncated {
		lines = append(lines, styledLine{
			Style: style.Foreground(app.theme.colors[colorMuted]),
			Text:  padRight("  … earlier output hidden; "+app.keys.hint(actionToolsExpand)+" expand", width),
		})
	}
	lines = append(lines, preview...)
	lines = append(lines, styledLine{Style: tcell.StyleDefault, Text: ""})

	return lines
}

func (app *App) renderExpandedToolBlock(width int, event *parsedToolEvent, style tcell.Style) []styledLine {
	lines := make([]styledLine, 0, 10)
	label := fmt.Sprintf("%s  %s collapse", toolTitle(event), app.keys.hint(actionToolsExpand))
	lines = append(lines,
		styledLine{Style: tcell.StyleDefault, Text: ""},
		styledLine{Style: style.Bold(true), Text: padRight(label, width)},
	)
	lines = append(lines, app.toolArgumentLines(width, event, style)...)
	lines = append(lines, app.toolDiffLines(width, event, style)...)
	lines = append(lines, app.toolOutputLines(width, event, style)...)
	lines = append(lines, styledLine{Style: tcell.StyleDefault, Text: ""})

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
	lines := []styledLine{{Style: style.Bold(true), Text: padRight("diff:", width)}}
	for _, line := range content {
		for _, wrapped := range wrapText(line.Text, innerWidth) {
			lines = append(lines, styledLine{Style: line.Style, Text: padRight("  "+wrapped, width)})
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
	lines = append(lines, styledLine{Style: style.Bold(true), Text: padRight(label+":", width)})

	return append(lines, contentLines...)
}

func indentedLines(width int, content string, style tcell.Style) []styledLine {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	innerWidth := max(1, width-2)
	lines := []styledLine{}
	for _, line := range strings.Split(content, "\n") {
		for _, wrapped := range wrapText(line, innerWidth) {
			lines = append(lines, styledLine{Style: style, Text: padRight("  "+wrapped, width)})
		}
	}

	return lines
}

func tailIndentedLines(width int, content string, style tcell.Style, limit int) ([]styledLine, bool) {
	lines := indentedLines(width, content, style)
	if limit <= 0 || len(lines) <= limit {
		return lines, false
	}

	return lines[len(lines)-limit:], true
}

func toolEventOutput(event *parsedToolEvent) string {
	output := strings.TrimSpace(event.Output)
	if event.Error != "" {
		output = strings.TrimSpace(event.Error + "\n" + output)
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
	for _, line := range strings.Split(content, "\n") {
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
	event.Error = strings.TrimSpace(strings.Join(sections[toolSectionError], "\n"))
	event.Output = strings.TrimSpace(strings.Join(sections[toolSectionOutput], "\n"))

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
