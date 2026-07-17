package terminal

import (
	"bytes"
	"encoding/json"
	"strings"

	udiff "github.com/aymanbagabas/go-udiff"
	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/tui"
)

const (
	maxCollapsedToolOutputLines = 5
	toolArgumentDiffContext     = 3
	toolSectionTool             = "tool"
	toolSectionParentCallID     = "parent_call_id"
	toolSectionArguments        = "arguments"
	toolSectionDetails          = "details"
	toolSectionError            = "error"
	toolSectionOutput           = "output"
	toolNameEdit                = "edit"
	pendingToolIndicator        = "◌"
)

type parsedToolEvent struct {
	Name          string
	ParentCallID  string
	ArgumentsJSON string
	DetailsJSON   string
	Error         string
	Output        string
}

func (app *App) renderToolBlock(width int, message chatMessage) []tui.Line {
	event := parseToolEventContent(message.Content, string(message.Role))
	display := toolDisplayFromParsedEvent(&event)

	return app.renderToolDisplayBlock(width, &display)
}

func (app *App) renderRunningToolBlock(width int, call *assistant.ToolCallEvent) []tui.Line {
	display := toolDisplayFromCall(call)

	return app.renderToolDisplayBlock(width, &display)
}

func (app *App) renderToolDisplayBlock(width int, display *toolDisplay) []tui.Line {
	style := app.toolDisplayStyle(display)
	if display.Status == toolDisplayPending {
		return app.renderPendingToolDisplay(width, display, style)
	}

	if !app.toolsExpanded {
		return app.renderCollapsedToolDisplay(width, display, style)
	}

	return app.renderExpandedToolDisplay(width, display, style)
}

func (app *App) toolDisplayStyle(display *toolDisplay) tcell.Style {
	if display.Status == toolDisplayError {
		return app.theme.background(colorToolErrorBg)
	}

	return app.theme.background(colorToolSuccessBg)
}

func (app *App) renderPendingToolDisplay(width int, display *toolDisplay, style tcell.Style) []tui.Line {
	lines := toolBlockStart(width, style)
	lines = append(lines, app.toolHeaderDisplayLines(width, display, style)...)

	if app.toolsExpanded {
		if source := executeSource(display); source != "" {
			lines = append(lines, app.toolExpandHintLines(width, style)...)
			lines = append(lines, app.toolCodeLines(width, source, style)...)
		}
	}

	lines = append(lines, toolBlockEnd(width, style)...)

	return lines
}

func (app *App) renderCollapsedToolDisplay(width int, display *toolDisplay, style tcell.Style) []tui.Line {
	lines := toolBlockStart(width, style)
	lines = append(lines, app.toolHeaderDisplayLines(width, display, style)...)

	preview, hiddenLines := tailIndentedLines(width, toolDisplayOutput(display), style, maxCollapsedToolOutputLines)
	if hiddenLines > 0 {
		lines = append(lines, paddedContentLine(
			width,
			hiddenToolLinesText(hiddenLines, app.keys.hint(actionToolsExpand)),
			style.Foreground(app.theme.colors[colorMuted]),
		))
	}

	lines = append(lines, preview...)
	lines = append(lines, toolBlockEnd(width, style)...)

	return lines
}

func (app *App) renderExpandedToolDisplay(width int, display *toolDisplay, style tcell.Style) []tui.Line {
	lines := make([]tui.Line, 0, initialToolBlockLines)
	lines = append(lines, toolBlockStart(width, style)...)
	lines = append(lines, app.toolHeaderDisplayLines(width, display, style)...)
	lines = append(lines, app.toolExpandHintLines(width, style)...)
	lines = append(lines, app.toolArgumentLines(width, display, style)...)
	lines = append(lines, app.toolDiffLines(width, display, style)...)
	lines = append(lines, app.toolOutputLines(width, display, style)...)
	lines = append(lines, toolBlockEnd(width, style)...)

	return lines
}

func (app *App) toolArgumentLines(width int, display *toolDisplay, style tcell.Style) []tui.Line {
	if source := executeSource(display); source != "" {
		return app.toolCodeLines(width, source, style)
	}

	arguments := prettyJSON(display.ArgumentsJSON)
	if arguments == "" {
		return nil
	}

	return plainSectionLines(width, "args", arguments, style)
}

func (app *App) toolCodeLines(width int, source string, style tcell.Style) []tui.Line {
	view := tui.CodeBlock{
		Style:    style,
		Engine:   &app.renderer.Lexer,
		Language: "go",
		Text:     source,
		Theme:    codeTheme(app.theme),
	}
	content := tui.PrefixLines(
		view.Render(max(1, toolContentWidth(width)), maxToolBlockRenderLines),
		"  ",
		style,
	)

	return app.styledSectionLines(width, "code", content, style)
}

func executeSource(display *toolDisplay) string {
	if display == nil {
		return ""
	}

	name := strings.TrimSpace(display.Name)
	if name != toolDisplayExecute && name != toolDisplayWorkflow {
		return ""
	}

	return rawTextArg(decodeToolArgs(display.ArgumentsJSON), "source")
}

func (app *App) toolDiffLines(width int, display *toolDisplay, style tcell.Style) []tui.Line {
	diff := toolDisplayDiff(display)
	if diff == "" {
		return nil
	}

	baseStyle := app.theme.background(colorCodeBg).Foreground(app.theme.colors[colorCodeText])
	view := tui.DiffView{Style: baseStyle, Text: diff, Theme: codeTheme(app.theme)}
	content := tui.PrefixLines(
		view.Render(max(1, toolContentWidth(width)), maxToolBlockRenderLines),
		"  ",
		baseStyle,
	)

	return app.styledSectionLines(width, "diff", content, style)
}

func (app *App) styledSectionLines(width int, label string, content []tui.Line, style tcell.Style) []tui.Line {
	lines := make([]tui.Line, 0, len(content)+1)
	lines = append(lines, paddedContentLine(width, label+":", style.Bold(true)))
	lines = append(lines, padLinesRight(content, width)...)

	return lines
}

func (app *App) toolOutputLines(width int, display *toolDisplay, style tcell.Style) []tui.Line {
	output := toolDisplayOutput(display)
	if output == "" {
		return nil
	}

	return plainSectionLines(width, "output", output, style)
}

func toolBlockStart(width int, style tcell.Style) []tui.Line {
	return []tui.Line{
		tui.NewLine(tcell.StyleDefault, ""),
		paddedContentLine(width, "", style),
	}
}

func toolBlockEnd(width int, style tcell.Style) []tui.Line {
	return []tui.Line{
		paddedContentLine(width, "", style),
		tui.NewLine(tcell.StyleDefault, ""),
	}
}

func (app *App) toolHeaderDisplayLines(width int, display *toolDisplay, style tcell.Style) []tui.Line {
	title := app.toolDisplayTitle(display)
	if display.Nested {
		title = "  ↳ " + title
	}

	return paddedContentLines(width, title, style.Bold(true), false)
}

func (app *App) toolExpandHintLines(width int, style tcell.Style) []tui.Line {
	hint := app.keys.hint(actionToolsExpand)
	if hint == "" {
		return nil
	}

	return paddedContentLines(width, hint+" collapse", style.Foreground(app.theme.colors[colorMuted]), false)
}

func plainSectionLines(width int, label, content string, style tcell.Style) []tui.Line {
	contentLines := indentedLines(width, content, style)
	lines := make([]tui.Line, 0, len(contentLines)+1)
	lines = append(lines, paddedContentLine(width, label+":", style.Bold(true)))

	return append(lines, contentLines...)
}

func padLinesRight(lines []tui.Line, width int) []tui.Line {
	padded := make([]tui.Line, 0, len(lines))
	for _, line := range lines {
		padding := max(0, width-line.Width())

		line.Text += strings.Repeat(" ", padding)
		if len(line.Spans) > 0 && padding > 0 {
			line.Spans = append(line.Spans, tui.Span{Style: line.Style, Text: strings.Repeat(" ", padding)})
		}

		padded = append(padded, line)
	}

	return padded
}

func indentedLines(width int, content string, style tcell.Style) []tui.Line {
	return contentLines(width, content, style, true)
}

func paddedContentLines(width int, content string, style tcell.Style, preserveWhitespace bool) []tui.Line {
	return contentLines(width, content, style, preserveWhitespace)
}

func contentLines(width int, content string, style tcell.Style, preserveWhitespace bool) []tui.Line {
	if strings.TrimSpace(content) == "" {
		return nil
	}

	contentWidth := toolContentWidth(width)
	lines := []tui.Line{}

	for line := range strings.SplitSeq(content, "\n") {
		wrappedLines := tui.Wrap(line, contentWidth)
		if preserveWhitespace {
			wrappedLines = tui.WrapPreserveWhitespace(line, contentWidth)
		}

		for _, wrapped := range wrappedLines {
			lines = append(lines, paddedContentLine(width, wrapped, style))
		}
	}

	return lines
}

func paddedContentLine(width int, content string, style tcell.Style) tui.Line {
	padding := strings.Repeat(" ", messageHorizontalPadding)

	return tui.NewLine(style, padding+tui.PadRight(content, toolContentWidth(width))+padding)
}

func toolContentWidth(width int) int {
	return max(1, width-messageBoxHorizontalPadding)
}

func tailIndentedLines(width int, content string, style tcell.Style, limit int) (tail []tui.Line, hidden int) {
	lines := indentedLines(width, content, style)
	if limit <= 0 || len(lines) <= limit {
		return lines, 0
	}

	hiddenLines := len(lines) - limit

	return lines[hiddenLines:], hiddenLines
}

func (app *App) renderToolMessageTail(width int, message chatMessage, rowsNeeded int) ([]tui.Line, bool) {
	if rowsNeeded <= 0 {
		return nil, true
	}

	event := parseToolEventContent(message.Content, string(message.Role))
	display := toolDisplayFromParsedEvent(&event)
	style := app.toolDisplayStyle(&display)

	tailBudget := max(0, rowsNeeded-messageOuterRows)
	if tailBudget == 0 {
		return []tui.Line{tui.NewLine(tcell.StyleDefault, "")}, false
	}

	tail, hidden := tailExpandedToolLines(width, &display, style, tailBudget)
	if hidden {
		return append(tail, toolBlockEnd(width, style)...), false
	}

	prefix := app.expandedToolPrefixLines(width, &display, style)
	lines := make([]tui.Line, 0, len(prefix)+len(tail)+messageOuterRows)
	lines = append(lines, prefix...)
	lines = append(lines, tail...)
	lines = append(lines, toolBlockEnd(width, style)...)

	if len(lines) <= rowsNeeded {
		return lines, true
	}

	return lines[len(lines)-rowsNeeded:], false
}

func (app *App) expandedToolPrefixLines(width int, display *toolDisplay, style tcell.Style) []tui.Line {
	lines := make([]tui.Line, 0, toolHeaderLines)
	lines = append(lines, toolBlockStart(width, style)...)
	lines = append(lines, app.toolHeaderDisplayLines(width, display, style)...)
	lines = append(lines, app.toolExpandHintLines(width, style)...)
	lines = append(lines, app.toolArgumentLines(width, display, style)...)
	lines = append(lines, app.toolDiffLines(width, display, style)...)

	return lines
}

func tailExpandedToolLines(
	width int,
	display *toolDisplay,
	style tcell.Style,
	rowsNeeded int,
) ([]tui.Line, bool) {
	if output := toolDisplayOutput(display); output != "" {
		return tailSectionLinesFromEnd(width, "output", output, style, rowsNeeded)
	}

	if display.DetailsJSON != "" {
		return tailSectionLinesFromEnd(width, "details", compactJSON(display.DetailsJSON), style, rowsNeeded)
	}

	if display.ArgumentsJSON != "" {
		return tailSectionLinesFromEnd(width, "args", prettyJSON(display.ArgumentsJSON), style, rowsNeeded)
	}

	return nil, false
}

func tailSectionLinesFromEnd(
	width int,
	label string,
	content string,
	style tcell.Style,
	rowsNeeded int,
) ([]tui.Line, bool) {
	if rowsNeeded <= 0 || strings.TrimSpace(content) == "" {
		return nil, false
	}

	if rowsNeeded == 1 {
		return []tui.Line{paddedContentLine(width, label+":", style.Bold(true))}, true
	}

	tail, hidden := tailIndentedLinesFromEnd(width, content, style, rowsNeeded-1)
	lines := make([]tui.Line, 0, len(tail)+1)
	lines = append(lines, paddedContentLine(width, label+":", style.Bold(true)))
	lines = append(lines, tail...)

	return lines, hidden
}

func tailIndentedLinesFromEnd(width int, content string, style tcell.Style, limit int) ([]tui.Line, bool) {
	if limit <= 0 || strings.TrimSpace(content) == "" {
		return nil, false
	}

	collector := tailLineCollector{
		Style:  style,
		Lines:  make([]tui.Line, 0, limit),
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
	Lines  []tui.Line
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
	wrapped := tui.WrapPreserveWhitespace(line, max(1, toolContentWidth(collector.Width)))
	added := 0

	for index := len(wrapped) - 1; index >= 0 && len(collector.Lines) < collector.Limit; index-- {
		collector.Lines = append(collector.Lines, paddedContentLine(collector.Width, wrapped[index], collector.Style))
		added++
	}

	if added < len(wrapped) {
		collector.Hidden = true
	}
}

func reverseStyledLines(lines []tui.Line) {
	for left, right := 0, len(lines)-1; left < right; left, right = left+1, right-1 {
		lines[left], lines[right] = lines[right], lines[left]
	}
}

func hiddenToolLinesText(hiddenLines int, expandHint string) string {
	unit := "lines"
	if hiddenLines == 1 {
		unit = "line"
	}

	text := "… " + tui.Int(hiddenLines) + " earlier output " + unit + " hidden"
	if expandHint != "" {
		text += "  " + expandHint + " expand"
	}

	return text
}

func compactJSON(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}

	var data any
	if err := json.Unmarshal([]byte(value), &data); err != nil {
		return strings.TrimSpace(value)
	}

	payload, err := json.Marshal(data)
	if err != nil {
		return strings.TrimSpace(value)
	}

	return string(payload)
}

func prettyJSON(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}

	var buffer bytes.Buffer
	if err := json.Indent(&buffer, []byte(value), "", "  "); err != nil {
		return strings.TrimSpace(value)
	}

	return strings.TrimSpace(buffer.String())
}

func toolDisplayOutput(display *toolDisplay) string {
	output := strings.Trim(display.Output, "\n")

	errorText := strings.Trim(display.Error, "\n")
	if errorText != "" && errorText != output {
		output = strings.Trim(errorText+"\n"+output, "\n")
	}

	return output
}

func parseToolEventContent(content, fallback string) parsedToolEvent {
	event := parsedToolEvent{
		Name:          fallback,
		ParentCallID:  "",
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

	event.ParentCallID = strings.TrimSpace(strings.Join(sections[toolSectionParentCallID], "\n"))
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
	case toolSectionTool, toolSectionParentCallID, toolSectionArguments, toolSectionDetails,
		toolSectionError, toolSectionOutput:
		return name, strings.TrimSpace(right), true
	default:
		return "", "", false
	}
}

func (app *App) toolDisplayTitle(display *toolDisplay) string {
	if display.Status == toolDisplayPending {
		return pendingToolIndicator + " " + fallbackToolName(display.Title)
	}

	return fallbackToolName(display.Title)
}

func toolDisplayDiff(display *toolDisplay) string {
	if display == nil {
		return ""
	}

	if diff := diffFromToolDetails(display.DetailsJSON); diff != "" {
		return diff
	}

	if strings.TrimSpace(display.Name) != toolNameEdit {
		return ""
	}

	return diffFromEditArguments(display.ArgumentsJSON)
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

func diffFromEditArguments(argumentsJSON string) string {
	args := decodeToolArgs(argumentsJSON)

	rawEdits, ok := args["edits"].([]any)
	if !ok || len(rawEdits) == 0 {
		return ""
	}

	diffs := make([]string, 0, len(rawEdits))
	for _, rawEdit := range rawEdits {
		editArgs, ok := rawEdit.(map[string]any)
		if !ok {
			continue
		}

		diff := diffFromEditArgumentTexts(
			rawTextArg(editArgs, "old_text", "oldText"),
			rawTextArg(editArgs, "new_text", "newText"),
		)
		if diff != "" {
			diffs = append(diffs, diff)
		}
	}

	return strings.TrimSpace(strings.Join(diffs, "\n"))
}

func diffFromEditArgumentTexts(oldText, newText string) string {
	if oldText == "" && newText == "" {
		return ""
	}

	edits := udiff.Lines(oldText, newText)
	if len(edits) == 0 {
		return ""
	}

	diff, err := udiff.ToUnifiedDiff("old_text", "new_text", oldText, edits, toolArgumentDiffContext)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(diff.String())
}

func rawTextArg(args map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := args[key].(string)
		if ok {
			return value
		}
	}

	return ""
}
