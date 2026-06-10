// Package panel provides a generic searchable selection panel model for the terminal UI.
package panel

import (
	"slices"
	"strings"

	"github.com/gdamore/tcell/v3"
	"github.com/samber/lo"

	"github.com/omarluq/librecode/internal/terminal/rendertext"
)

// Kind identifies the app-specific panel using this generic model.
type Kind string

// Item is one selectable row in a panel.
type Item struct {
	Value       string
	Title       string
	Description string
	Meta        string
}

// Model is a searchable selection panel.
type Model struct {
	kind        Kind
	title       string
	subtitle    string
	query       string
	items       []Item
	filtered    []Item
	selected    int
	searchable  bool
	showDetails bool
}

// New returns a selection panel model.
func New(kind Kind, title, subtitle string, items []Item, searchable bool) *Model {
	model := &Model{
		kind:        kind,
		title:       title,
		subtitle:    subtitle,
		query:       "",
		items:       slices.Clone(items),
		filtered:    []Item{},
		selected:    0,
		searchable:  searchable,
		showDetails: true,
	}
	model.ApplyFilter()

	return model
}

// Kind returns the app-specific panel kind.
func (model *Model) Kind() Kind {
	if model == nil {
		return ""
	}

	return model.kind
}

// Items returns a copy of the original item list.
func (model *Model) Items() []Item {
	if model == nil {
		return []Item{}
	}

	return slices.Clone(model.items)
}

// FilteredItems returns a copy of the visible item list.
func (model *Model) FilteredItems() []Item {
	if model == nil {
		return []Item{}
	}

	return slices.Clone(model.filtered)
}

// SelectedIndex returns the currently selected visible row.
func (model *Model) SelectedIndex() int {
	if model == nil {
		return 0
	}

	return model.selected
}

// SetSelectedIndex updates the selected visible row, clamping to the valid range.
func (model *Model) SetSelectedIndex(index int) {
	if model == nil {
		return
	}
	if len(model.filtered) == 0 {
		model.selected = 0
		return
	}
	model.selected = min(max(0, index), len(model.filtered)-1)
}

// SelectedValue returns the selected item's value.
func (model *Model) SelectedValue() (string, bool) {
	if model == nil || len(model.filtered) == 0 {
		return "", false
	}

	return model.filtered[model.selected].Value, true
}

// SelectedItem returns the selected item.
func (model *Model) SelectedItem() (Item, bool) {
	if model == nil || len(model.filtered) == 0 {
		return Item{Value: "", Title: "", Description: "", Meta: ""}, false
	}

	return model.filtered[model.selected], true
}

// MoveSelection moves the selected row by delta, wrapping around the visible list.
func (model *Model) MoveSelection(delta int) {
	if model == nil {
		return
	}
	if len(model.filtered) == 0 {
		model.selected = 0
		return
	}
	model.selected += delta
	for model.selected < 0 {
		model.selected += len(model.filtered)
	}
	model.selected %= len(model.filtered)
}

// AppendQueryRune appends a searchable rune to the panel query.
func (model *Model) AppendQueryRune(char rune) {
	if model == nil || !model.searchable || char == 0 {
		return
	}
	model.query += string(char)
	model.ApplyFilter()
}

// BackspaceQuery removes one rune from the query.
func (model *Model) BackspaceQuery() {
	if model == nil || model.query == "" {
		return
	}
	queryRunes := []rune(model.query)
	model.query = string(queryRunes[:len(queryRunes)-1])
	model.ApplyFilter()
}

// ApplyFilter refreshes visible items from the current query.
func (model *Model) ApplyFilter() {
	if model == nil {
		return
	}
	if strings.TrimSpace(model.query) == "" {
		model.filtered = slices.Clone(model.items)
		model.selected = min(model.selected, max(0, len(model.filtered)-1))
		return
	}
	query := strings.ToLower(strings.TrimSpace(model.query))
	model.filtered = lo.Filter(model.items, func(item Item, _ int) bool {
		return itemMatchesQuery(item, query)
	})
	model.selected = min(model.selected, max(0, len(model.filtered)-1))
}

func itemMatchesQuery(item Item, query string) bool {
	haystack := strings.ToLower(strings.Join([]string{item.Title, item.Description, item.Meta, item.Value}, " "))
	parts := strings.FieldsSeq(query)
	for part := range parts {
		if !strings.Contains(haystack, part) {
			return false
		}
	}

	return true
}

// Styles is the style set used to render a panel.
type Styles struct {
	Border   tcell.Style
	Accent   tcell.Style
	Muted    tcell.Style
	Text     tcell.Style
	Selected tcell.Style
	Dim      tcell.Style
}

// Hints are already-renderable key names for panel help text.
type Hints struct {
	Up      string
	Down    string
	Confirm string
	Cancel  string
}

// RenderOptions configures panel rendering.
type RenderOptions struct {
	Styles Styles
	Hints  Hints
	Width  int
	Height int
}

// Render converts the model into styled terminal lines.
func (model *Model) Render(options *RenderOptions) []rendertext.Line {
	if model == nil || options == nil {
		return []rendertext.Line{}
	}
	width := max(1, options.Width)
	height := max(1, options.Height)
	contentWidth := max(1, width-4)
	maxItems := max(1, height-8)
	lines := make([]rendertext.Line, 0, min(height, maxItems+8))
	lines = append(lines,
		rendertext.NewLine(options.Styles.Border, rendertext.TopBorder(width, "")),
		rendertext.NewLine(options.Styles.Accent.Bold(true), panelRow(model.title, contentWidth)),
	)
	if model.subtitle != "" {
		lines = append(lines, rendertext.NewLine(options.Styles.Muted, panelRow(model.subtitle, contentWidth)))
	}
	if model.searchable {
		query := "Search: " + model.query
		lines = append(lines, rendertext.NewLine(options.Styles.Text, panelRow(query, contentWidth)))
	}
	lines = append(lines, rendertext.NewLine(options.Styles.Border, rendertext.MiddleBorder(width)))
	lines = append(lines, model.itemLines(contentWidth, maxItems, &options.Styles)...)
	lines = append(lines,
		model.hintLine(contentWidth, width, &options.Styles, &options.Hints),
		rendertext.NewLine(options.Styles.Border, rendertext.BottomBorder(width)),
	)

	return rendertext.SafeTail(lines, height)
}

func panelRow(text string, width int) string {
	return "│ " + rendertext.PadRight(text, width) + " │"
}

func (model *Model) itemLines(contentWidth, maxItems int, styles *Styles) []rendertext.Line {
	if len(model.filtered) == 0 {
		return []rendertext.Line{rendertext.NewLine(styles.Muted, panelRow("No matches", contentWidth))}
	}
	startIndex := model.windowStart(maxItems)
	endIndex := min(startIndex+maxItems, len(model.filtered))
	lines := make([]rendertext.Line, 0, endIndex-startIndex)
	for index := startIndex; index < endIndex; index++ {
		lines = append(lines, model.itemLine(index, contentWidth, styles))
	}

	return lines
}

func (model *Model) itemLine(index, width int, styles *Styles) rendertext.Line {
	item := model.filtered[index]
	prefix := "  "
	style := styles.Text
	if index == model.selected {
		prefix = "→ "
		style = styles.Selected
	}
	text := prefix + item.Title
	if item.Meta != "" {
		text += " " + item.Meta
	}
	if model.showDetails && item.Description != "" {
		text += " — " + item.Description
	}

	return rendertext.NewLine(style, panelRow(text, width))
}

func (model *Model) windowStart(maxItems int) int {
	if len(model.filtered) <= maxItems {
		return 0
	}
	startIndex := model.selected - maxItems/2
	startIndex = max(0, startIndex)
	startIndex = min(startIndex, len(model.filtered)-maxItems)

	return startIndex
}

func (model *Model) hintLine(contentWidth, width int, styles *Styles, hints *Hints) rendertext.Line {
	position := ""
	if len(model.filtered) > 0 {
		position = " " + rendertext.Truncate(model.positionText(), max(0, width/4))
	}
	hint := hints.Up + "/" + hints.Down + " navigate · " +
		hints.Confirm + " select · " + hints.Cancel + " cancel" + position

	return rendertext.NewLine(styles.Dim, panelRow(hint, contentWidth))
}

func (model *Model) positionText() string {
	return "(" + rendertext.Int(model.selected+1) + "/" + rendertext.Int(len(model.filtered)) + ")"
}
