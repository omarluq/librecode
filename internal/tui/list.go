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

	list.selected = clampSelection(index, len(list.filtered))
}

// MoveSelection moves the selected row by delta, wrapping around the visible list.
func (list *List) MoveSelection(delta int) {
	if list == nil {
		return
	}

	list.selected = moveSelection(list.selected, delta, len(list.filtered))
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
		list.selected = clampSelection(list.selected, len(list.filtered))

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
	list.selected = clampSelection(list.selected, len(list.filtered))
}

func clampSelection(index, count int) int {
	if count <= 0 {
		return 0
	}

	return min(max(0, index), count-1)
}

func moveSelection(index, delta, count int) int {
	if count <= 0 {
		return 0
	}

	index += delta
	for index < 0 {
		index += count
	}

	return index % count
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

// Autocomplete is a compact selectable list for completion popups.
type Autocomplete struct {
	items    []ListItem
	selected int
}

// AutocompleteStyles is the style set used to render an autocomplete popup.
type AutocompleteStyles struct {
	Header   tcell.Style
	Text     tcell.Style
	Selected tcell.Style
}

// AutocompleteRenderOptions configures autocomplete popup rendering.
type AutocompleteRenderOptions struct {
	Styles         AutocompleteStyles
	Header         string
	ItemPrefix     string
	SelectedPrefix string
	Width          int
	MaxItems       int
	LabelWidth     int
}

// NewAutocomplete returns an autocomplete popup model.
func NewAutocomplete(items []ListItem) *Autocomplete {
	autocomplete := &Autocomplete{items: []ListItem{}, selected: 0}
	autocomplete.SetItems(items)

	return autocomplete
}

// SetItems replaces completion items, preserving the selected row when possible.
func (autocomplete *Autocomplete) SetItems(items []ListItem) {
	if autocomplete == nil {
		return
	}

	autocomplete.items = slices.Clone(items)
	autocomplete.selected = clampSelection(autocomplete.selected, len(autocomplete.items))
}

// Items returns a copy of the completion item list.
func (autocomplete *Autocomplete) Items() []ListItem {
	if autocomplete == nil {
		return []ListItem{}
	}

	return slices.Clone(autocomplete.items)
}

// SelectedIndex returns the selected row index.
func (autocomplete *Autocomplete) SelectedIndex() int {
	if autocomplete == nil {
		return 0
	}

	return autocomplete.selected
}

// SetSelectedIndex updates the selected row, clamping to the valid range.
func (autocomplete *Autocomplete) SetSelectedIndex(index int) {
	if autocomplete == nil {
		return
	}

	autocomplete.selected = clampSelection(index, len(autocomplete.items))
}

// MoveSelection moves the selected row by delta, wrapping around the item list.
func (autocomplete *Autocomplete) MoveSelection(delta int) {
	if autocomplete == nil {
		return
	}

	autocomplete.selected = moveSelection(autocomplete.selected, delta, len(autocomplete.items))
}

// SelectedItem returns the selected completion item.
func (autocomplete *Autocomplete) SelectedItem() (ListItem, bool) {
	if autocomplete == nil || len(autocomplete.items) == 0 {
		return ListItem{Value: "", Title: "", Description: "", Meta: ""}, false
	}

	index := clampSelection(autocomplete.selected, len(autocomplete.items))

	return autocomplete.items[index], true
}

// Render returns styled terminal lines for the current autocomplete state.
func (autocomplete *Autocomplete) Render(options *AutocompleteRenderOptions) []Line {
	if autocomplete == nil || options == nil || len(autocomplete.items) == 0 || options.Width <= 0 {
		return []Line{}
	}

	limit := autocompleteRenderLimit(options.MaxItems, len(autocomplete.items))
	start := autocomplete.windowStart(limit)
	lines := make([]Line, 0, limit+1)

	if options.Header != "" {
		lines = append(lines, NewLine(options.Styles.Header, PadRight(options.Header, options.Width)))
	}

	labelWidth := autocompleteLabelWidth(autocomplete.items[start:start+limit], options.LabelWidth)
	for offset := range limit {
		index := start + offset
		style := options.Styles.Text

		prefix := autocompleteItemPrefix(options.ItemPrefix, "  ")
		if index == autocomplete.selected {
			style = options.Styles.Selected
			prefix = autocompleteItemPrefix(options.SelectedPrefix, "› ")
		}

		text := autocompleteItemText(autocomplete.items[index], prefix, labelWidth)
		lines = append(lines, NewLine(style, PadRight(text, options.Width)))
	}

	return lines
}

func autocompleteRenderLimit(maxItems, count int) int {
	if maxItems <= 0 || maxItems > count {
		return count
	}

	return maxItems
}

func (autocomplete *Autocomplete) windowStart(limit int) int {
	if limit <= 0 || len(autocomplete.items) <= limit || autocomplete.selected < limit {
		return 0
	}

	return min(autocomplete.selected-limit+1, len(autocomplete.items)-limit)
}

func autocompleteLabelWidth(items []ListItem, requested int) int {
	if requested > 0 {
		return requested
	}

	width := 0
	for _, item := range items {
		width = max(width, Width(autocompleteLabel(item)))
	}

	return width
}

func autocompleteItemText(item ListItem, prefix string, labelWidth int) string {
	text := prefix + PadRight(autocompleteLabel(item), labelWidth)
	if item.Meta != "" {
		text += " " + item.Meta
	}

	if item.Description != "" {
		text += " " + item.Description
	}

	return text
}

func autocompleteLabel(item ListItem) string {
	if item.Title != "" {
		return item.Title
	}

	return item.Value
}

func autocompleteItemPrefix(value, fallback string) string {
	if value != "" {
		return value
	}

	return fallback
}
