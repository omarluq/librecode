package tui

import (
	"slices"
	"strings"

	"github.com/gdamore/tcell/v3"
)

// ListItem is one selectable row in a list.
type ListItem struct {
	Value       string
	Title       string
	Description string
	Meta        string
}

// List is a searchable selectable list model and renderer.
type List struct {
	Title       string
	Subtitle    string
	Query       string
	Items       []ListItem
	Filtered    []ListItem
	Selected    int
	Searchable  bool
	ShowDetails bool
}

// NewList returns a searchable selection list.
func NewList(title, subtitle string, items []ListItem, searchable bool) *List {
	list := &List{
		Title:       title,
		Subtitle:    subtitle,
		Query:       "",
		Items:       slices.Clone(items),
		Filtered:    []ListItem{},
		Selected:    0,
		Searchable:  searchable,
		ShowDetails: true,
	}
	list.ApplyFilter()

	return list
}

// SelectedItem returns the selected visible item.
func (list *List) SelectedItem() (ListItem, bool) {
	if list == nil || len(list.Filtered) == 0 {
		return ListItem{Value: "", Title: "", Description: "", Meta: ""}, false
	}

	index := min(max(0, list.Selected), len(list.Filtered)-1)

	return list.Filtered[index], true
}

// SelectedValue returns the selected item's value.
func (list *List) SelectedValue() (string, bool) {
	item, ok := list.SelectedItem()
	if !ok {
		return "", false
	}

	return item.Value, true
}

// SetSelectedIndex updates the selected visible row, clamping to the valid range.
func (list *List) SetSelectedIndex(index int) {
	if list == nil {
		return
	}

	if len(list.Filtered) == 0 {
		list.Selected = 0

		return
	}

	list.Selected = min(max(0, index), len(list.Filtered)-1)
}

// MoveSelection moves the selected row by delta, wrapping around the visible list.
func (list *List) MoveSelection(delta int) {
	if list == nil {
		return
	}

	if len(list.Filtered) == 0 {
		list.Selected = 0

		return
	}

	list.Selected += delta
	for list.Selected < 0 {
		list.Selected += len(list.Filtered)
	}

	list.Selected %= len(list.Filtered)
}

// AppendQueryRune appends a searchable rune to the query.
func (list *List) AppendQueryRune(char rune) {
	if list == nil || !list.Searchable || char == 0 {
		return
	}

	list.Query += string(char)
	list.ApplyFilter()
}

// BackspaceQuery removes one rune from the query.
func (list *List) BackspaceQuery() {
	if list == nil || list.Query == "" {
		return
	}

	queryRunes := []rune(list.Query)
	list.Query = string(queryRunes[:len(queryRunes)-1])
	list.ApplyFilter()
}

// ApplyFilter refreshes visible items from the current query.
func (list *List) ApplyFilter() {
	if list == nil {
		return
	}

	if strings.TrimSpace(list.Query) == "" {
		list.Filtered = slices.Clone(list.Items)
		list.Selected = min(list.Selected, max(0, len(list.Filtered)-1))

		return
	}

	query := strings.ToLower(strings.TrimSpace(list.Query))

	filtered := make([]ListItem, 0, len(list.Items))
	for _, item := range list.Items {
		if listItemMatchesQuery(item, query) {
			filtered = append(filtered, item)
		}
	}

	list.Filtered = filtered
	list.Selected = min(list.Selected, max(0, len(list.Filtered)-1))
}

func listItemMatchesQuery(item ListItem, query string) bool {
	haystack := strings.ToLower(strings.Join([]string{item.Title, item.Description, item.Meta, item.Value}, " "))
	for part := range strings.FieldsSeq(query) {
		if !strings.Contains(haystack, part) {
			return false
		}
	}

	return true
}

// ListStyles is the style set used to render a list.
type ListStyles struct {
	Border   tcell.Style
	Accent   tcell.Style
	Muted    tcell.Style
	Text     tcell.Style
	Selected tcell.Style
	Dim      tcell.Style
}

// ListHints are key names for list help text.
type ListHints struct {
	Up      string
	Down    string
	Confirm string
	Cancel  string
}

// ListRenderOptions configures list rendering.
type ListRenderOptions struct {
	Styles ListStyles
	Hints  ListHints
	Width  int
	Height int
}

const (
	listHorizontalPadding    = 4
	listChromeRows           = 8
	listCenterDivisor        = 2
	listPositionWidthDivisor = 4
)

// Render returns styled terminal lines for the current list state.
func (list *List) Render(options *ListRenderOptions) []Line {
	if list == nil {
		return []Line{}
	}

	if options == nil {
		return []Line{}
	}

	width := max(1, options.Width)
	height := max(1, options.Height)
	contentWidth := max(1, width-listHorizontalPadding)
	maxItems := max(1, height-listChromeRows)
	lines := make([]Line, 0, min(height, maxItems+listChromeRows))

	lines = append(lines,
		NewLine(options.Styles.Border, TopBorder(width, "")),
		NewLine(options.Styles.Accent.Bold(true), listRow(list.Title, contentWidth)),
	)
	if list.Subtitle != "" {
		lines = append(lines, NewLine(options.Styles.Muted, listRow(list.Subtitle, contentWidth)))
	}

	if list.Searchable {
		lines = append(lines, NewLine(options.Styles.Text, listRow("Search: "+list.Query, contentWidth)))
	}

	lines = append(lines, NewLine(options.Styles.Border, MiddleBorder(width)))
	lines = append(lines, list.itemLines(contentWidth, maxItems, &options.Styles)...)
	lines = append(lines,
		list.hintLine(contentWidth, width, &options.Styles, options.Hints),
		NewLine(options.Styles.Border, BottomBorder(width)),
	)

	return Tail(lines, height)
}

// Draw draws the list into rect.
func (list *List) Draw(screen ContentSetter, rect Rect, styles *ListStyles, hints ListHints) {
	options := ListRenderOptions{Styles: safeListStyles(styles), Hints: hints, Width: rect.Width, Height: rect.Height}
	DrawLines(screen, rect, list.Render(&options))
}

func listRow(text string, width int) string {
	return "│ " + PadRight(text, width) + " │"
}

func safeListStyles(styles *ListStyles) ListStyles {
	if styles == nil {
		return ListStyles{
			Border:   tcell.Style{},
			Accent:   tcell.Style{},
			Muted:    tcell.Style{},
			Text:     tcell.Style{},
			Selected: tcell.Style{},
			Dim:      tcell.Style{},
		}
	}

	return *styles
}

func (list *List) itemLines(contentWidth, maxItems int, styles *ListStyles) []Line {
	if len(list.Filtered) == 0 {
		return []Line{NewLine(styles.Muted, listRow("No matches", contentWidth))}
	}

	startIndex := list.windowStart(maxItems)
	endIndex := min(startIndex+maxItems, len(list.Filtered))

	lines := make([]Line, 0, endIndex-startIndex)
	for index := startIndex; index < endIndex; index++ {
		lines = append(lines, list.itemLine(index, contentWidth, styles))
	}

	return lines
}

func (list *List) itemLine(index, width int, styles *ListStyles) Line {
	if index < 0 || index >= len(list.Filtered) {
		return NewLine(styles.Muted, listRow("", width))
	}

	item := list.Filtered[index]
	prefix := "  "
	style := styles.Text

	if index == list.Selected {
		prefix = "→ "
		style = styles.Selected
	}

	text := prefix + item.Title
	if item.Meta != "" {
		text += " " + item.Meta
	}

	if list.ShowDetails && item.Description != "" {
		text += " — " + item.Description
	}

	return NewLine(style, listRow(text, width))
}

func (list *List) windowStart(maxItems int) int {
	if len(list.Filtered) <= maxItems {
		return 0
	}

	startIndex := list.Selected - maxItems/listCenterDivisor
	startIndex = max(0, startIndex)
	startIndex = min(startIndex, len(list.Filtered)-maxItems)

	return startIndex
}

func (list *List) hintLine(contentWidth, width int, styles *ListStyles, hints ListHints) Line {
	position := ""

	if len(list.Filtered) > 0 {
		counter := "(" + Int(list.Selected+1) + "/" + Int(len(list.Filtered)) + ")"
		position = " " + Truncate(counter, max(0, width/listPositionWidthDivisor))
	}

	hint := hints.Up + "/" + hints.Down + " navigate · " +
		hints.Confirm + " select · " + hints.Cancel + " cancel" + position

	return NewLine(styles.Dim, listRow(hint, contentWidth))
}

// SelectList is an alias for List when used as a picker.
type SelectList = List

// Autocomplete is a compact list variant for completion popups.
type Autocomplete struct {
	*List
}

// NewAutocomplete returns an autocomplete popup model.
func NewAutocomplete(items []ListItem) *Autocomplete {
	return &Autocomplete{List: NewList("", "", items, true)}
}
