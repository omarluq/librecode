package terminal

import (
	"context"
	"strings"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/extension"
)

const (
	windowRendererDefault   = "default"
	windowRendererExtension = "extension"
)

func (app *App) draw(ctx context.Context) {
	width, height := app.screen.Size()
	app.frame = newCellBuffer(width, height, tcell.StyleDefault)
	if width < 20 || height < 8 {
		app.drawTiny(width, height)
		app.flushFrame()
		return
	}

	layout := app.currentRuntimeLayout()
	app.runRenderExtensions(ctx, &layout)
	layout = app.currentRuntimeLayout()
	if app.mode == modePanel && app.panel != nil {
		app.drawPanelWindow(&layout)
	} else {
		app.drawTranscriptWindow(&layout)
	}
	app.drawAutocompleteWindow(&layout)
	app.drawComposerWindow(&layout)
	app.drawStatusWindow(&layout)
	app.applyUIOverrides(&layout)
	app.showRuntimeCursor(&layout)
	app.flushFrame()
}

func (app *App) flushFrame() {
	app.renderer.flush(app.frame)
	app.screen.Show()
}

func (app *App) drawTiny(width, height int) {
	message := truncateText("librecode: terminal too small", width)
	writeLine(app.frame, max(0, height/2), width, message, app.theme.style(colorWarning))
}

func (app *App) drawTranscriptWindow(layout *runtimeLayout) {
	window := layout.Transcript
	if !window.Visible || window.Height <= 0 || app.extensionOwnsWindow(window.Name) {
		return
	}
	if buffer, ok := app.runtimeBufferOverride(window.Buffer); ok {
		app.drawRuntimeTextBuffer(&window, &buffer, app.theme.style(colorText))
		return
	}
	if app.showWelcomeOnly() {
		app.drawWelcomeOnly(window.Width, window.Height, window.Y)
		return
	}
	lines := app.messageLines(window.Width, window.Height)
	for index, line := range lines {
		app.writeStyledLine(window.Y+index, window.Width, line)
	}
}

func (app *App) drawPanelWindow(layout *runtimeLayout) {
	window := layout.Transcript
	if !window.Visible || window.Height <= 0 || app.extensionOwnsWindow(window.Name) {
		return
	}
	lines := app.panel.render(window.Width, window.Height, app.theme, app.keys)
	for index, line := range lines {
		app.writeStyledLine(window.Y+index, window.Width, line)
	}
}

func (app *App) messageLines(width, maxRows int) []styledLine {
	return app.visibleMessageLineGroups(app.messageLineGroups(width), maxRows)
}

func (app *App) messageLineGroups(width int) [][]styledLine {
	groups := make([][]styledLine, 0, len(app.messages)+len(app.streamingBlocks)+3)
	for index := range app.messages {
		groups = append(groups, app.cachedMessageLines(width, index))
	}
	if len(app.streamingBlocks) > 0 {
		for index := range app.streamingBlocks {
			groups = append(groups, app.cachedStreamingBlockLines(width, index))
		}
	} else {
		if app.streamingThinkingText != "" {
			groups = append(groups, app.renderStreamingThinkingMessage(width, app.streamingThinkingText))
		}
		if app.streamingText != "" {
			groups = append(groups, app.renderStreamingMessage(width, app.streamingText))
		}
	}
	if app.working {
		groups = append(groups, []styledLine{{Style: app.theme.style(colorAccent), Text: app.workingIndicator()}})
	}
	if len(app.queuedMessages) > 0 {
		groups = append(groups, app.renderQueuedMessages(width))
	}

	return groups
}

func (app *App) cachedMessageLines(width, index int) []styledLine {
	app.ensureMessageLineCache(width)
	if index < len(app.messageLineCache) && app.messageLineCache[index].Valid {
		return app.messageLineCache[index].Lines
	}
	lines := app.renderMessage(width, app.messages[index])
	if index >= len(app.messageLineCache) {
		return lines
	}
	app.messageLineCache[index] = cachedRenderedMessage{Lines: lines, Valid: true}

	return lines
}

func (app *App) ensureMessageLineCache(width int) {
	app.ensureLineCache(width, len(app.messages), &app.messageLineCache, &app.messageLineCacheState)
}

func (app *App) ensureLineCache(
	width int,
	targetLength int,
	cache *[]cachedRenderedMessage,
	cacheState *messageLineCacheState,
) {
	state := app.currentLineCacheState(width)
	if *cacheState != state {
		*cache = nil
		*cacheState = state
	}
	if len(*cache) > targetLength {
		*cache = (*cache)[:targetLength]
	}
	for len(*cache) < targetLength {
		*cache = append(*cache, emptyCachedRenderedMessage())
	}
}

func (app *App) currentLineCacheState(width int) messageLineCacheState {
	return messageLineCacheState{
		ThemeName:     app.theme.name,
		Width:         width,
		HideThinking:  app.hideThinking,
		ToolsExpanded: app.toolsExpanded,
	}
}

func (app *App) cachedStreamingBlockLines(width, index int) []styledLine {
	app.ensureStreamingBlockLineCache(width)
	if index < len(app.streamingBlockLineCache) && app.streamingBlockLineCache[index].Valid {
		return app.streamingBlockLineCache[index].Lines
	}
	lines := app.renderStreamingBlockMessage(width, app.streamingBlocks[index])
	if index >= len(app.streamingBlockLineCache) {
		return lines
	}
	app.streamingBlockLineCache[index] = cachedRenderedMessage{Lines: lines, Valid: true}

	return lines
}

func (app *App) ensureStreamingBlockLineCache(width int) {
	app.ensureLineCache(
		width,
		len(app.streamingBlocks),
		&app.streamingBlockLineCache,
		&app.streamingBlockLineCacheState,
	)
}

func (app *App) visibleMessageLineGroups(groups [][]styledLine, maxRows int) []styledLine {
	totalRows := 0
	for _, group := range groups {
		totalRows += len(group)
	}
	if maxRows < 0 || totalRows <= maxRows {
		app.scrollOffset = 0
		return flattenStyledLineGroups(groups, totalRows)
	}
	maxOffset := max(0, totalRows-maxRows)
	app.scrollOffset = min(app.scrollOffset, maxOffset)
	end := totalRows - app.scrollOffset
	start := max(0, end-maxRows)

	return sliceStyledLineGroups(groups, start, end)
}

func flattenStyledLineGroups(groups [][]styledLine, totalRows int) []styledLine {
	lines := make([]styledLine, 0, totalRows)
	for _, group := range groups {
		lines = append(lines, group...)
	}

	return lines
}

func sliceStyledLineGroups(groups [][]styledLine, start, end int) []styledLine {
	lines := make([]styledLine, 0, max(0, end-start))
	offset := 0
	for _, group := range groups {
		nextOffset := offset + len(group)
		if nextOffset > start && offset < end {
			groupStart := max(0, start-offset)
			groupEnd := min(len(group), end-offset)
			lines = append(lines, group[groupStart:groupEnd]...)
		}
		offset = nextOffset
		if offset >= end {
			break
		}
	}

	return lines
}

func (app *App) renderMessage(width int, message chatMessage) []styledLine {
	switch message.Role {
	case database.RoleUser:
		return app.renderUserMessage(width, message.Content)
	case database.RoleAssistant:
		return app.renderAssistantMessage(width, message.Content)
	case database.RoleToolResult, database.RoleBashExecution:
		return app.renderToolMessage(width, message)
	case database.RoleThinking:
		return app.renderThinkingMessage(width, message)
	case database.RoleCustom:
		return app.renderCustomMessage(width, message.Content)
	case database.RoleBranchSummary, database.RoleCompactionSummary:
		return app.renderSummaryMessage(width, message)
	}

	return app.renderCustomMessage(width, message.Content)
}

func (app *App) renderUserMessage(width int, content string) []styledLine {
	innerWidth := max(1, width-4)
	wrapped := wrapText(content, innerWidth)
	lines := make([]styledLine, 0, len(wrapped)+4)
	lines = append(lines,
		styledLine{Style: app.theme.style(colorDim), Text: ""},
		styledLine{Style: app.theme.background(colorUserMessageBg), Text: padRight("", width)},
	)
	for _, line := range wrapped {
		text := "  " + padRight(line, innerWidth) + "  "
		lines = append(lines, styledLine{Style: app.theme.background(colorUserMessageBg), Text: text})
	}
	lines = append(lines,
		styledLine{Style: app.theme.background(colorUserMessageBg), Text: padRight("", width)},
		styledLine{Style: app.theme.style(colorDim), Text: ""},
	)

	return lines
}

func (app *App) renderQueuedMessages(width int) []styledLine {
	style := app.theme.background(colorUserMessageBg).Foreground(app.theme.colors[colorMuted])
	innerWidth := max(1, width-4)
	lines := []styledLine{{Style: app.theme.style(colorDim), Text: ""}}
	for index, message := range app.queuedMessages {
		header := "queued follow-up " + intText(index+1)
		lines = append(lines, styledLine{Style: style.Bold(true), Text: "  " + padRight(header, innerWidth) + "  "})
		for _, line := range wrapText(message, innerWidth) {
			lines = append(lines, styledLine{Style: style, Text: "  " + padRight(line, innerWidth) + "  "})
		}
	}
	lines = append(lines, styledLine{Style: app.theme.style(colorDim), Text: ""})

	return lines
}

func (app *App) renderAssistantMessage(width int, content string) []styledLine {
	markdownLines := app.renderMarkdown(strings.TrimSpace(content), width)
	lines := make([]styledLine, 0, len(markdownLines)+2)
	lines = append(lines, styledLine{Style: app.theme.style(colorDim), Text: ""})
	lines = append(lines, markdownLines...)
	lines = append(lines, styledLine{Style: app.theme.style(colorDim), Text: ""})

	return lines
}

func (app *App) renderStreamingMessage(width int, content string) []styledLine {
	wrapped := wrapText(strings.TrimSpace(content), width)
	style := app.theme.style(colorText)
	lines := make([]styledLine, 0, len(wrapped)+2)
	lines = append(lines, styledLine{Style: app.theme.style(colorDim), Text: ""})
	for _, line := range wrapped {
		lines = append(lines, styledLine{Style: style, Text: line})
	}
	lines = append(lines, styledLine{Style: app.theme.style(colorDim), Text: ""})

	return lines
}

func (app *App) renderStreamingThinkingMessage(width int, content string) []styledLine {
	return app.renderThinkingMessage(width, newChatMessage(database.RoleThinking, content))
}

func (app *App) renderStreamingBlockMessage(width int, message chatMessage) []styledLine {
	switch message.Role {
	case database.RoleAssistant:
		return app.renderStreamingMessage(width, message.Content)
	case database.RoleThinking:
		return app.renderStreamingThinkingMessage(width, message.Content)
	case database.RoleToolResult, database.RoleBashExecution:
		return app.renderToolMessage(width, message)
	case database.RoleUser,
		database.RoleCustom,
		database.RoleBranchSummary,
		database.RoleCompactionSummary:
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
			{Style: tcell.StyleDefault, Text: ""},
			{Style: style, Text: "thinking…"},
			{Style: tcell.StyleDefault, Text: ""},
		}
	}

	markdownLines := app.renderMarkdown(strings.TrimSpace(message.Content), width)
	lines := make([]styledLine, 0, len(markdownLines)+3)
	lines = append(lines,
		styledLine{Style: tcell.StyleDefault, Text: ""},
		styledLine{Style: style.Bold(true), Text: settingThinking},
	)
	for _, line := range markdownLines {
		lines = append(lines, styledLine{Style: style, Text: line.Text})
	}
	lines = append(lines, styledLine{Style: app.theme.style(colorDim), Text: ""})

	return lines
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
		styledLine{Style: tcell.StyleDefault, Text: ""},
		styledLine{Style: style, Text: padRight("", width)},
		styledLine{Style: style.Bold(true), Text: padRight("  ["+label+"]", width)},
	)
	for _, line := range wrapped {
		lines = append(lines, styledLine{Style: style, Text: "  " + padRight(line, innerWidth) + "  "})
	}
	lines = append(lines,
		styledLine{Style: style, Text: padRight("", width)},
		styledLine{Style: tcell.StyleDefault, Text: ""},
	)

	return lines
}

func (app *App) drawAutocompleteWindow(layout *runtimeLayout) {
	window := layout.Autocomplete
	if !window.Visible || window.Height <= 0 || app.extensionOwnsWindow(window.Name) {
		return
	}
	lines := app.autocompleteLines(window.Width)
	for index, line := range lines {
		writeLine(app.frame, window.Y+index, window.Width, line.Text, line.Style)
	}
}

func (app *App) drawComposerWindow(layout *runtimeLayout) {
	window := layout.Composer
	if !window.Visible || window.Height <= 0 || app.extensionOwnsWindow(window.Name) {
		return
	}
	editor := app.renderComposerEditor(window.Width, max(1, window.Height-2))
	borderStyle := app.theme.style(app.editorBorderColor())
	for index, line := range editor.Lines {
		writeEditorLine(app.frame, window.Y+index, window.Width, line, index, len(editor.Lines), borderStyle)
	}
	window.CursorRow = editor.CursorRow
	window.CursorCol = editor.CursorCol
	app.runtimeWindows[window.Name] = window
}

func (app *App) renderComposerEditor(width, bodyRows int) editorRender {
	return renderEditor(
		[]rune(app.composerText()),
		app.composerCursor(),
		width,
		bodyRows,
		app.theme,
		app.editorBorderColor(),
		app.composerBorderLabel(),
	)
}

func (app *App) drawStatusWindow(layout *runtimeLayout) {
	window := layout.Status
	if !window.Visible || window.Height <= 0 || app.extensionOwnsWindow(window.Name) {
		return
	}
	lines := app.footerLines(window.Width)
	if buffer, ok := app.runtimeBufferOverride(window.Buffer); ok {
		lines = app.renderBufferTextLines(window.Width, buffer.Text, app.theme.style(colorDim))
	}
	for index, line := range lines {
		if index >= window.Height {
			return
		}
		writeLine(app.frame, window.Y+index, window.Width, line.Text, line.Style)
	}
}

func (app *App) composerReserve(width, height int) int {
	layout := app.defaultRuntimeLayout(width, height)
	return height - layout.Transcript.Height
}

func (app *App) currentRuntimeLayout() runtimeLayout {
	width, height := 80, 24
	if app.screen != nil {
		width, height = app.screen.Size()
	}
	layout := app.defaultRuntimeLayout(width, height)
	return app.mergeRuntimeLayout(&layout)
}

func (app *App) defaultRuntimeLayout(width, height int) runtimeLayout {
	statusLines := app.footerLines(width)
	autocompleteLines := app.autocompleteLines(width)
	maxComposerHeight := min(defaultEditorRows, max(3, height-len(statusLines)-len(autocompleteLines)-2))
	maxComposerHeight = max(3, maxComposerHeight)
	composerHeight := len(app.renderComposerEditor(width, max(1, maxComposerHeight-2)).Lines)
	reservedRows := len(statusLines) + len(autocompleteLines) + composerHeight
	if reservedRows > height {
		composerHeight = max(3, height-len(statusLines)-len(autocompleteLines))
		reservedRows = len(statusLines) + len(autocompleteLines) + composerHeight
	}
	transcriptHeight := max(0, height-reservedRows)
	autocompleteStart := transcriptHeight
	composerStart := autocompleteStart + len(autocompleteLines)
	statusStart := height - len(statusLines)

	return runtimeLayout{
		Windows: nil,
		Width:   width,
		Height:  height,
		Transcript: extension.WindowState{
			Metadata:  map[string]any{extensionMetadataCount: len(app.messages)},
			Name:      extensionBufferTranscript,
			Role:      extensionBufferTranscript,
			Buffer:    extensionBufferTranscript,
			Renderer:  windowRendererDefault,
			X:         0,
			Y:         0,
			Width:     width,
			Height:    transcriptHeight,
			CursorRow: 0,
			CursorCol: 0,
			Visible:   true,
		},
		Autocomplete: extension.WindowState{
			Metadata:  map[string]any{},
			Name:      "autocomplete",
			Role:      "autocomplete",
			Buffer:    extensionBufferStatus,
			Renderer:  windowRendererDefault,
			X:         0,
			Y:         autocompleteStart,
			Width:     width,
			Height:    len(autocompleteLines),
			CursorRow: 0,
			CursorCol: 0,
			Visible:   len(autocompleteLines) > 0,
		},
		Composer: extension.WindowState{
			Metadata:  map[string]any{},
			Name:      extensionBufferComposer,
			Role:      extensionBufferComposer,
			Buffer:    extensionBufferComposer,
			Renderer:  windowRendererDefault,
			X:         0,
			Y:         composerStart,
			Width:     width,
			Height:    composerHeight,
			CursorRow: 0,
			CursorCol: 0,
			Visible:   true,
		},
		Status: extension.WindowState{
			Metadata:  map[string]any{},
			Name:      extensionBufferStatus,
			Role:      extensionBufferStatus,
			Buffer:    extensionBufferStatus,
			Renderer:  windowRendererDefault,
			X:         0,
			Y:         statusStart,
			Width:     width,
			Height:    len(statusLines),
			CursorRow: 0,
			CursorCol: 0,
			Visible:   true,
		},
	}
}

func (app *App) mergeRuntimeLayout(layout *runtimeLayout) runtimeLayout {
	windows := app.cloneRuntimeWindows(layout)
	transcript := windows[extensionBufferTranscript]
	autocomplete := windows["autocomplete"]
	composer := windows[extensionBufferComposer]
	status := windows[extensionBufferStatus]

	return runtimeLayout{
		Width:        layout.Width,
		Height:       layout.Height,
		Transcript:   transcript,
		Autocomplete: autocomplete,
		Composer:     composer,
		Status:       status,
		Windows:      windows,
	}
}

func (app *App) extensionOwnsWindow(name string) bool {
	window, ok := app.runtimeWindows[name]
	if !ok {
		return false
	}

	return strings.EqualFold(strings.TrimSpace(window.Renderer), windowRendererExtension)
}

func (app *App) editorBorderColor() colorToken {
	if strings.HasPrefix(strings.TrimSpace(app.composerText()), "!") {
		return colorBashMode
	}
	switch app.currentThinkingLevel() {
	case "minimal", "low":
		return colorBorderMuted
	case "medium", "high", "xhigh":
		return colorBorderAccent
	default:
		return colorBorder
	}
}

func (app *App) footerLines(width int) []styledLine {
	lineTexts := app.defaultStatusLineTexts()
	lines := make([]styledLine, 0, len(lineTexts))
	for _, lineText := range lineTexts {
		lines = append(lines, styledLine{Style: app.theme.style(colorDim), Text: truncateText(lineText, width)})
	}

	return lines
}

func (app *App) writeStyledLine(row, width int, line styledLine) {
	writeLineWithVerticalBorders(app.frame, row, width, line.Text, line.Style, app.theme.colors[colorBorderMuted])
}

func writeLine(screen cellTarget, row, width int, text string, style tcell.Style) {
	writeTextAt(screen, 0, row, width, text, style)
}

func writeTextAt(screen cellTarget, column, row, width int, text string, style tcell.Style) {
	if row < 0 || column < 0 || width <= 0 {
		return
	}
	line := []rune(truncateText(text, width))
	for index := 0; index < width; index++ {
		value := ' '
		if index < len(line) {
			value = line[index]
		}
		screen.SetContent(column+index, row, value, nil, style)
	}
}

func writeLineWithVerticalBorders(
	screen cellTarget,
	row int,
	width int,
	text string,
	style tcell.Style,
	borderColor tcell.Color,
) {
	if row < 0 {
		return
	}
	line := []rune(truncateText(text, width))
	for index := 0; index < width; index++ {
		value := ' '
		if index < len(line) {
			value = line[index]
		}
		cellStyle := style
		if isVerticalBorderCell(value, index, width) {
			cellStyle = style.Foreground(borderColor)
		}
		screen.SetContent(index, row, value, nil, cellStyle)
	}
}

func isVerticalBorderCell(value rune, index, width int) bool {
	return value == '│' && (index == 0 || index == width-1)
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
	bodyRunes := []rune(truncateText(line.Text, width))
	for index := 0; index < width; index++ {
		value := ' '
		if index < len(bodyRunes) {
			value = bodyRunes[index]
		}
		style := line.Style
		if index < 2 || index >= max(0, width-2) {
			style = borderStyle
		}
		screen.SetContent(index, row, value, nil, style)
	}
}

func (app *App) cloneRuntimeWindows(layout *runtimeLayout) map[string]extension.WindowState {
	if app.runtimeLayout != nil && len(app.runtimeLayout.Windows) > 0 {
		return cloneWindowStates(app.runtimeLayout.Windows)
	}
	windows := map[string]extension.WindowState{
		layout.Transcript.Name:   layout.Transcript,
		layout.Autocomplete.Name: layout.Autocomplete,
		layout.Composer.Name:     layout.Composer,
		layout.Status.Name:       layout.Status,
	}
	for name := range app.runtimeWindows {
		windows[name] = app.runtimeWindows[name]
	}
	for name := range windows {
		window := windows[name]
		if window.Name == "" {
			window.Name = name
		}
		windows[name] = window
	}

	return windows
}

func cloneWindowStates(windows map[string]extension.WindowState) map[string]extension.WindowState {
	cloned := make(map[string]extension.WindowState, len(windows))
	for name := range windows {
		window := windows[name]
		if window.Name == "" {
			window.Name = name
		}
		if window.Metadata == nil {
			window.Metadata = map[string]any{}
		}
		cloned[name] = window
	}

	return cloned
}

func (app *App) applyUIOverrides(layout *runtimeLayout) {
	windows := app.cloneRuntimeWindows(layout)
	for name, override := range app.uiWindowOverrides {
		window, ok := windows[name]
		if !ok || !window.Visible {
			continue
		}
		if override.Reset {
			clearWindow(app.frame, &window)
		}
		for index := range override.DrawOps {
			drawOp := &override.DrawOps[index]
			app.drawUIWindowText(&window, drawOp)
		}
	}
}

func (app *App) drawUIWindowText(window *extension.WindowState, drawOp *extension.UIDrawOp) {
	style := app.uiStyle(drawOp.Style)
	writeTextAt(
		app.frame,
		window.X+drawOp.Col,
		window.Y+drawOp.Row,
		window.Width-drawOp.Col,
		drawOp.Text,
		style,
	)
}

func clearWindow(target cellTarget, window *extension.WindowState) {
	style := tcell.StyleDefault
	for row := 0; row < window.Height; row++ {
		writeLine(target, window.Y+row, window.Width, "", style)
	}
}

func (app *App) uiStyle(style extension.UIStyle) tcell.Style {
	resolved := tcell.StyleDefault
	if style.FG != "" {
		resolved = resolved.Foreground(app.namedUIColor(style.FG))
	}
	if style.BG != "" {
		resolved = resolved.Background(app.namedUIColor(style.BG))
	}
	if style.Bold {
		resolved = resolved.Bold(true)
	}
	if style.Italic {
		resolved = resolved.Italic(true)
	}

	return resolved
}

func (app *App) namedUIColor(name string) tcell.Color {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "accent":
		return app.theme.colors[colorAccent]
	case "border":
		return app.theme.colors[colorBorder]
	case "muted":
		return app.theme.colors[colorMuted]
	case "dim":
		return app.theme.colors[colorDim]
	case "text", "white", "default":
		return app.theme.colors[colorText]
	case "warning":
		return app.theme.colors[colorWarning]
	case "error":
		return app.theme.colors[colorError]
	case "success":
		return app.theme.colors[colorSuccess]
	default:
		return tcell.ColorDefault
	}
}

func (app *App) showRuntimeCursor(layout *runtimeLayout) {
	if app.screen == nil {
		return
	}
	windows := app.cloneRuntimeWindows(layout)
	if app.uiCursor != nil {
		if window, ok := windows[app.uiCursor.Window]; ok {
			app.screen.ShowCursor(window.X+app.uiCursor.Col, window.Y+app.uiCursor.Row)
			return
		}
	}
	composer, ok := windows[extensionBufferComposer]
	if !ok {
		app.screen.HideCursor()
		return
	}
	app.screen.ShowCursor(composer.X+composer.CursorCol, composer.Y+composer.CursorRow)
}
