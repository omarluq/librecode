package terminal

import (
	"strings"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/transcript"
)

func (app *App) cachedStreamingBlockLines(width, index int) []styledLine {
	app.ensureStreamingBlockLineCache(width)
	if index < len(app.transcript.Streaming.LineCache) && app.transcript.Streaming.LineCache[index].Valid {
		return app.transcript.Streaming.LineCache[index].Lines
	}
	lines := app.renderStreamingBlockMessage(width, app.transcript.Streaming.Blocks[index])
	if index >= len(app.transcript.Streaming.LineCache) {
		return lines
	}
	app.transcript.Streaming.LineCache[index] = cachedRenderedMessage{Lines: lines, Valid: true}

	return lines
}

func (app *App) ensureStreamingBlockLineCache(width int) {
	app.ensureLineCache(
		width,
		len(app.transcript.Streaming.Blocks),
		&app.transcript.Streaming.LineCache,
		&app.transcript.Streaming.CacheState,
	)
}

func (app *App) renderMessage(width int, message chatMessage) []styledLine {
	switch message.Role {
	case transcript.RoleUser:
		return app.renderUserMessage(width, message.Content)
	case transcript.RoleAssistant:
		return app.renderAssistantMessage(width, message.Content)
	case transcript.RoleToolResult, transcript.RoleBashExecution:
		return app.renderToolMessage(width, message)
	case transcript.RoleThinking:
		return app.renderThinkingMessage(width, message)
	case transcript.RoleCustom:
		return app.renderCustomMessage(width, message.Content)
	case transcript.RoleBranchSummary, transcript.RoleCompactionSummary:
		return app.renderSummaryMessage(width, message)
	}

	return app.renderCustomMessage(width, message.Content)
}

func (app *App) renderUserMessage(width int, content string) []styledLine {
	innerWidth := max(1, width-4)
	wrapped := wrapText(content, innerWidth)
	lines := make([]styledLine, 0, len(wrapped)+4)
	lines = append(lines,
		newStyledLine(app.theme.style(colorDim), ""),
		newStyledLine(app.theme.background(colorUserMessageBg), padRight("", width)),
	)
	for _, line := range wrapped {
		text := "  " + padRight(line, innerWidth) + "  "
		lines = append(lines, newStyledLine(app.theme.background(colorUserMessageBg), text))
	}
	lines = append(lines,
		newStyledLine(app.theme.background(colorUserMessageBg), padRight("", width)),
		newStyledLine(app.theme.style(colorDim), ""),
	)

	return lines
}

func (app *App) renderQueuedMessages(width int) []styledLine {
	style := app.theme.background(colorUserMessageBg).Foreground(app.theme.colors[colorMuted])
	innerWidth := max(1, width-4)
	lines := []styledLine{newStyledLine(app.theme.style(colorDim), "")}
	for index, message := range app.queuedMessages {
		header := "queued follow-up " + intText(index+1)
		lines = append(lines, newStyledLine(style.Bold(true), "  "+padRight(header, innerWidth)+"  "))
		for _, line := range wrapText(message, innerWidth) {
			lines = append(lines, newStyledLine(style, "  "+padRight(line, innerWidth)+"  "))
		}
	}
	lines = append(lines, newStyledLine(app.theme.style(colorDim), ""))

	return lines
}

func (app *App) renderAssistantMessage(width int, content string) []styledLine {
	markdownLines := app.renderMarkdown(strings.TrimSpace(content), width)
	lines := make([]styledLine, 0, len(markdownLines)+2)
	lines = append(lines, newStyledLine(app.theme.style(colorDim), ""))
	lines = append(lines, markdownLines...)
	lines = append(lines, newStyledLine(app.theme.style(colorDim), ""))

	return lines
}

func (app *App) renderStreamingMessage(width int, content string) []styledLine {
	wrapped := wrapText(strings.TrimSpace(content), width)
	style := app.theme.style(colorText)
	lines := make([]styledLine, 0, len(wrapped)+2)
	lines = append(lines, newStyledLine(app.theme.style(colorDim), ""))
	for _, line := range wrapped {
		lines = append(lines, newStyledLine(style, line))
	}
	lines = append(lines, newStyledLine(app.theme.style(colorDim), ""))

	return lines
}

func (app *App) renderStreamingThinkingMessage(width int, content string) []styledLine {
	return app.renderThinkingMessage(width, newChatMessage(transcript.RoleThinking, content))
}

func (app *App) renderStreamingBlockMessage(width int, message chatMessage) []styledLine {
	switch message.Role {
	case transcript.RoleAssistant:
		return app.renderStreamingMessage(width, message.Content)
	case transcript.RoleThinking:
		return app.renderStreamingThinkingMessage(width, message.Content)
	case transcript.RoleToolResult, transcript.RoleBashExecution:
		return app.renderToolMessage(width, message)
	case transcript.RoleUser,
		transcript.RoleCustom,
		transcript.RoleBranchSummary,
		transcript.RoleCompactionSummary:
		return app.renderMessage(width, message)
	}

	return app.renderMessage(width, message)
}

func (app *App) renderToolMessage(width int, message chatMessage) []styledLine {
	return app.renderToolBlock(width, message)
}

func (app *App) renderThinkingMessage(width int, message chatMessage) []styledLine {
	style := app.theme.style(colorDim).Italic(true)
	if app.hideThinking {
		return []styledLine{
			newStyledLine(tcell.StyleDefault, ""),
			newStyledLine(style, "thinking…"),
			newStyledLine(tcell.StyleDefault, ""),
		}
	}

	markdownLines := app.renderMarkdown(strings.TrimSpace(message.Content), width)
	lines := make([]styledLine, 0, len(markdownLines)+3)
	lines = append(lines,
		newStyledLine(tcell.StyleDefault, ""),
		newStyledLine(style.Bold(true), settingThinking),
	)
	for _, line := range markdownLines {
		lines = append(lines, mergeLineStyle(line, style))
	}
	lines = append(lines, newStyledLine(app.theme.style(colorDim), ""))

	return lines
}

func mergeLineStyle(line styledLine, style tcell.Style) styledLine {
	merged := line
	merged.Style = mergeStyles(line.Style, style)
	if len(line.Spans) > 0 {
		merged.Spans = make([]styledSpan, len(line.Spans))
		for index, span := range line.Spans {
			merged.Spans[index] = styledSpan{Style: mergeStyles(span.Style, style), Text: span.Text}
		}
	}

	return merged
}

func mergeStyles(base, overlay tcell.Style) tcell.Style {
	merged := base
	if overlay.HasBold() {
		merged = merged.Bold(true)
	}
	if overlay.HasItalic() {
		merged = merged.Italic(true)
	}
	if overlay.HasUnderline() {
		merged = merged.Underline(true)
	}
	if overlay.HasReverse() {
		merged = merged.Reverse(true)
	}
	if fg := overlay.GetForeground(); fg != tcell.ColorDefault {
		merged = merged.Foreground(fg)
	}
	if bg := overlay.GetBackground(); bg != tcell.ColorDefault {
		merged = merged.Background(bg)
	}

	return merged
}

func (app *App) renderCustomMessage(width int, content string) []styledLine {
	if isWelcomeMessage(content) {
		return app.renderWelcomeMessage(width, content)
	}

	return boxedLines(width, "system", content, app.theme.background(colorCustomMessageBg))
}

func (app *App) renderSummaryMessage(width int, message chatMessage) []styledLine {
	return boxedLines(width, string(message.Role), message.Content, app.theme.style(colorMuted))
}

func boxedLines(width int, label, content string, style tcell.Style) []styledLine {
	innerWidth := max(1, width-4)
	wrapped := wrapText(content, innerWidth)
	lines := make([]styledLine, 0, len(wrapped)+5)
	lines = append(lines,
		newStyledLine(tcell.StyleDefault, ""),
		newStyledLine(style, padRight("", width)),
		newStyledLine(style.Bold(true), padRight("  ["+label+"]", width)),
	)
	for _, line := range wrapped {
		lines = append(lines, newStyledLine(style, "  "+padRight(line, innerWidth)+"  "))
	}
	lines = append(lines,
		newStyledLine(style, padRight("", width)),
		newStyledLine(tcell.StyleDefault, ""),
	)

	return lines
}
