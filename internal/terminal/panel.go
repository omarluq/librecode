package terminal

import (
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v3"
)

type panelKind string

const (
	panelModel        panelKind = "model"
	panelScopedModels panelKind = "scoped_models"
	panelAuthLogin    panelKind = "auth_login"
	panelAuthLogout   panelKind = "auth_logout"
	panelSettings     panelKind = "settings"
	panelSessions     panelKind = "sessions"
	panelTree         panelKind = "tree"
)

type panelItem struct {
	Value       string
	Title       string
	Description string
	Meta        string
}

type selectionPanel struct {
	kind        panelKind
	title       string
	subtitle    string
	query       string
	items       []panelItem
	filtered    []panelItem
	selected    int
	searchable  bool
	showDetails bool
}

type panelActionType string

const (
	panelActionNone   panelActionType = "none"
	panelActionCancel panelActionType = "cancel"
	panelActionSelect panelActionType = "select"
)

type panelAction struct {
	Type  panelActionType
	Value string
}

func newSelectionPanel(
	kind panelKind,
	title string,
	subtitle string,
	items []panelItem,
	searchable bool,
) *selectionPanel {
	panel := &selectionPanel{
		kind:        kind,
		title:       title,
		subtitle:    subtitle,
		query:       "",
		items:       items,
		filtered:    []panelItem{},
		selected:    0,
		searchable:  searchable,
		showDetails: true,
	}
	panel.applyFilter()

	return panel
}

func (panel *selectionPanel) handleKey(event *tcell.EventKey, bindings *keybindings) panelAction {
	if bindings.matches(event, actionSelectCancel) {
		return panelAction{Type: panelActionCancel, Value: ""}
	}
	if bindings.matches(event, actionSelectConfirm) {
		return panel.selectedAction()
	}
	if bindings.matches(event, actionSelectUp) {
		panel.moveSelection(-1)
		return panelAction{Type: panelActionNone, Value: ""}
	}
	if bindings.matches(event, actionSelectDown) {
		panel.moveSelection(1)
		return panelAction{Type: panelActionNone, Value: ""}
	}
	if bindings.matches(event, actionSelectPageUp) {
		panel.moveSelection(-10)
		return panelAction{Type: panelActionNone, Value: ""}
	}
	if bindings.matches(event, actionSelectPageDown) {
		panel.moveSelection(10)
		return panelAction{Type: panelActionNone, Value: ""}
	}
	if panel.searchable {
		panel.handleSearchKey(event)
	}

	return panelAction{Type: panelActionNone, Value: ""}
}

func (panel *selectionPanel) selectedAction() panelAction {
	if value, ok := panel.selectedValue(); ok {
		return panelAction{Type: panelActionSelect, Value: value}
	}

	return panelAction{Type: panelActionNone, Value: ""}
}

func (panel *selectionPanel) selectedValue() (string, bool) {
	if len(panel.filtered) == 0 {
		return "", false
	}

	return panel.filtered[panel.selected].Value, true
}

func (panel *selectionPanel) selectedItem() (panelItem, bool) {
	if len(panel.filtered) == 0 {
		return panelItem{Value: "", Title: "", Description: "", Meta: ""}, false
	}

	return panel.filtered[panel.selected], true
}

func (panel *selectionPanel) moveSelection(delta int) {
	if len(panel.filtered) == 0 {
		panel.selected = 0
		return
	}
	panel.selected += delta
	for panel.selected < 0 {
		panel.selected += len(panel.filtered)
	}
	panel.selected %= len(panel.filtered)
}

func (panel *selectionPanel) handleSearchKey(event *tcell.EventKey) {
	if event.Key() == tcell.KeyBackspace || event.Key() == tcell.KeyBackspace2 {
		panel.backspaceQuery()
		return
	}
	if event.Key() == tcell.KeyRune {
		panel.query += string(eventRune(event))
		panel.applyFilter()
	}
}

func (panel *selectionPanel) backspaceQuery() {
	if panel.query == "" {
		return
	}
	queryRunes := []rune(panel.query)
	panel.query = string(queryRunes[:len(queryRunes)-1])
	panel.applyFilter()
}

func (panel *selectionPanel) applyFilter() {
	if strings.TrimSpace(panel.query) == "" {
		panel.filtered = append([]panelItem{}, panel.items...)
		panel.selected = min(panel.selected, max(0, len(panel.filtered)-1))
		return
	}
	query := strings.ToLower(strings.TrimSpace(panel.query))
	panel.filtered = []panelItem{}
	for _, item := range panel.items {
		if itemMatchesQuery(item, query) {
			panel.filtered = append(panel.filtered, item)
		}
	}
	panel.selected = min(panel.selected, max(0, len(panel.filtered)-1))
}

func itemMatchesQuery(item panelItem, query string) bool {
	haystack := strings.ToLower(strings.Join([]string{item.Title, item.Description, item.Meta, item.Value}, " "))
	parts := strings.Fields(query)
	for _, part := range parts {
		if !strings.Contains(haystack, part) {
			return false
		}
	}

	return true
}

func panelRow(text string, width int) string {
	return "│ " + padRight(text, width) + " │"
}

func (panel *selectionPanel) render(width, height int, theme terminalTheme, bindings *keybindings) []styledLine {
	contentWidth := max(1, width-4)
	maxItems := max(1, height-8)
	lines := make([]styledLine, 0, min(height, maxItems+8))
	borderStyle := theme.style(colorBorder)
	lines = append(lines,
		styledLine{Style: borderStyle, Text: editorTopBorder(width)},
		styledLine{Style: theme.style(colorAccent).Bold(true), Text: panelRow(panel.title, contentWidth)},
	)
	if panel.subtitle != "" {
		lines = append(lines, styledLine{Style: theme.style(colorMuted), Text: panelRow(panel.subtitle, contentWidth)})
	}
	if panel.searchable {
		query := "Search: " + panel.query
		lines = append(lines, styledLine{Style: theme.style(colorText), Text: panelRow(query, contentWidth)})
	}
	lines = append(lines, styledLine{Style: borderStyle, Text: "├" + strings.Repeat("─", max(1, width-2)) + "┤"})
	lines = append(lines, panel.itemLines(contentWidth, maxItems, theme)...)
	lines = append(lines,
		panel.hintLine(contentWidth, width, theme, bindings),
		styledLine{Style: borderStyle, Text: editorBottomBorder(width)},
	)

	return safeSlice(lines, height)
}

func (panel *selectionPanel) itemLines(contentWidth, maxItems int, theme terminalTheme) []styledLine {
	if len(panel.filtered) == 0 {
		return []styledLine{{Style: theme.style(colorMuted), Text: panelRow("No matches", contentWidth)}}
	}
	startIndex := panel.windowStart(maxItems)
	endIndex := min(startIndex+maxItems, len(panel.filtered))
	lines := make([]styledLine, 0, endIndex-startIndex)
	for index := startIndex; index < endIndex; index++ {
		lines = append(lines, panel.itemLine(index, contentWidth, theme))
	}

	return lines
}

func (panel *selectionPanel) itemLine(index, width int, theme terminalTheme) styledLine {
	item := panel.filtered[index]
	prefix := "  "
	style := theme.style(colorText)
	if index == panel.selected {
		prefix = "→ "
		style = theme.selected()
	}
	text := prefix + item.Title
	if item.Meta != "" {
		text += " " + item.Meta
	}
	if panel.showDetails && item.Description != "" {
		text += " — " + item.Description
	}

	return styledLine{Style: style, Text: panelRow(text, width)}
}

func (panel *selectionPanel) windowStart(maxItems int) int {
	if len(panel.filtered) <= maxItems {
		return 0
	}
	startIndex := panel.selected - maxItems/2
	startIndex = max(0, startIndex)
	startIndex = min(startIndex, len(panel.filtered)-maxItems)

	return startIndex
}

func (panel *selectionPanel) hintLine(
	contentWidth int,
	width int,
	theme terminalTheme,
	bindings *keybindings,
) styledLine {
	position := ""
	if len(panel.filtered) > 0 {
		position = " " + truncateText(panel.positionText(), max(0, width/4))
	}
	hint := bindings.hint(actionSelectUp) + "/" + bindings.hint(actionSelectDown) + " navigate · " +
		bindings.hint(actionSelectConfirm) + " select · " + bindings.hint(actionSelectCancel) + " cancel" + position

	return styledLine{Style: theme.style(colorDim), Text: panelRow(hint, contentWidth)}
}

func (panel *selectionPanel) positionText() string {
	return "(" + intText(panel.selected+1) + "/" + intText(len(panel.filtered)) + ")"
}

func intText(value int) string {
	return strconv.Itoa(value)
}
