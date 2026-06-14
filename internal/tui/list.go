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
	query       string
	items       []ListItem
	filtered    []ListItem
	selected    int
	ShowDetails bool
	searchable  bool
}

// NewList returns a searchable selection list.
func NewList(title, subtitle string, items []ListItem, searchable bool) *List {
	list := &List{
		Title:       title,
		Subtitle:    subtitle,
		ShowDetails: true,
		searchable:  searchable,
		query:       "",
		items:       slices.Clone(items),
		filtered:    []ListItem{},
		selected:    0,
	}
	list.ApplyFilter()

	return list
}

// Items returns a copy of the full item list.
func (list *List) Items() []ListItem {
	if list == nil {
		return []ListItem{}
	}

	return slices.Clone(list.items)
}

// FilteredItems returns a copy of the visible item list.
func (list *List) FilteredItems() []ListItem {
	if list == nil {
		return []ListItem{}
	}

	return slices.Clone(list.filtered)
}

// SelectedIndex returns the selected visible row index.
func (list *List) SelectedIndex() int {
	if list == nil {
		return 0
	}

	return list.selected
}

// Searchable reports whether query editing is enabled.
func (list *List) Searchable() bool {
	return list != nil && list.searchable
}

// Query returns the current search query.
func (list *List) Query() string {
	if list == nil {
		return ""
	}

	return list.query
}

// SelectedItem returns the selected visible item.
func (list *List) SelectedItem() (ListItem, bool) {
	if list == nil || len(list.filtered) == 0 {
		return ListItem{Value: "", Title: "", Description: "", Meta: ""}, false
	}

	index := min(max(0, list.selected), len(list.filtered)-1)

	return list.filtered[index], true
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

	if len(list.filtered) == 0 {
		list.selected = 0

		return
	}

	list.selected = min(max(0, index), len(list.filtered)-1)
}

// MoveSelection moves the selected row by delta, wrapping around the visible list.
func (list *List) MoveSelection(delta int) {
	if list == nil {
		return
	}

	if len(list.filtered) == 0 {
		list.selected = 0

		return
	}

	list.selected += delta
	for list.selected < 0 {
		list.selected += len(list.filtered)
	}

	list.selected %= len(list.filtered)
}

// AppendQueryRune appends a searchable rune to the query.
func (list *List) AppendQueryRune(char rune) {
	if list == nil || !list.searchable || char == 0 {
		return
	}

	list.query += string(char)
	list.ApplyFilter()
}

// BackspaceQuery removes one rune from the query.
func (list *List) BackspaceQuery() {
	if list == nil || list.query == "" {
		return
	}

	queryRunes := []rune(list.query)
	list.query = string(queryRunes[:len(queryRunes)-1])
	list.ApplyFilter()
}

// ApplyFilter refreshes visible items from the current query.
func (list *List) ApplyFilter() {
	if list == nil {
		return
	}

	if strings.TrimSpace(list.query) == "" {
		list.filtered = slices.Clone(list.items)
		list.selected = min(list.selected, max(0, len(list.filtered)-1))

		return
	}

	query := strings.ToLower(strings.TrimSpace(list.query))

	filtered := make([]ListItem, 0, len(list.items))
	for _, item := range list.items {
		if listItemMatchesQuery(item, query) {
			filtered = append(filtered, item)
		}
	}

	list.filtered = filtered
	list.selected = min(list.selected, max(0, len(list.filtered)-1))
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
	if list == nil || options == nil {
		return []Line{}
	}

	width := max(1, options.Width)
	height := max(1, options.Height)
	contentWidth := max(1, width-listHorizontalPadding)
	maxItems := max(1, height-listChromeRows)
	lines := make([]Line, 0, min(height, maxItems+listChromeRows))

	lines = append(lines,
		NewLine(options.Styles.Border, TopBorder(width, "")),
		listRow(list.Title, contentWidth, options.Styles.Accent.Bold(true), options.Styles.Border),
	)
	if list.Subtitle != "" {
		lines = append(lines, listRow(list.Subtitle, contentWidth, options.Styles.Muted, options.Styles.Border))
	}

	if list.searchable {
		lines = append(lines, listRow("Search: "+list.query, contentWidth, options.Styles.Text, options.Styles.Border))
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

func listRow(text string, width int, contentStyle, borderStyle tcell.Style) Line {
	return Line{
		Text:  "│ " + PadRight(text, width) + " │",
		Style: contentStyle,
		Spans: []Span{
			{Text: "│", Style: borderStyle},
			{Text: " " + PadRight(text, width) + " ", Style: contentStyle},
			{Text: "│", Style: borderStyle},
		},
	}
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
	if len(list.filtered) == 0 {
		return []Line{listRow("No matches", contentWidth, styles.Muted, styles.Border)}
	}

	startIndex := list.windowStart(maxItems)
	endIndex := min(startIndex+maxItems, len(list.filtered))

	lines := make([]Line, 0, endIndex-startIndex)
	for index := startIndex; index < endIndex; index++ {
		lines = append(lines, list.itemLine(index, contentWidth, styles))
	}

	return lines
}

func (list *List) itemLine(index, width int, styles *ListStyles) Line {
	if index < 0 || index >= len(list.filtered) {
		return listRow("", width, styles.Muted, styles.Border)
	}

	item := list.filtered[index]
	prefix := "  "
	style := styles.Text

	if index == list.selected {
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

	return listRow(text, width, style, styles.Border)
}

func (list *List) windowStart(maxItems int) int {
	if len(list.filtered) <= maxItems {
		return 0
	}

	startIndex := list.selected - maxItems/listCenterDivisor
	startIndex = max(0, startIndex)
	startIndex = min(startIndex, len(list.filtered)-maxItems)

	return startIndex
}

func (list *List) hintLine(contentWidth, width int, styles *ListStyles, hints ListHints) Line {
	position := ""

	if len(list.filtered) > 0 {
		counter := "(" + Int(list.selected+1) + "/" + Int(len(list.filtered)) + ")"
		position = " " + Truncate(counter, max(0, width/listPositionWidthDivisor))
	}

	hint := hints.Up + "/" + hints.Down + " navigate · " +
		hints.Confirm + " select · " + hints.Cancel + " cancel" + position

	return listRow(hint, contentWidth, styles.Dim, styles.Border)
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
