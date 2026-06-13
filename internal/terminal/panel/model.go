// Package panel adapts the reusable TUI list component to app-specific panel kinds.
package panel

import "github.com/omarluq/librecode/internal/tui"

// Kind identifies the app-specific panel using this generic model.
type Kind string

// Item is one selectable row in a panel.
type Item = tui.ListItem

// Styles is the style set used to render a panel.
type Styles = tui.ListStyles

// Hints are already-renderable key names for panel help text.
type Hints = tui.ListHints

// RenderOptions configures panel rendering.
type RenderOptions = tui.ListRenderOptions

// Model is a searchable selection panel.
type Model struct {
	list *tui.List
	kind Kind
}

// New returns a selection panel model.
func New(kind Kind, title, subtitle string, items []Item, searchable bool) *Model {
	return &Model{
		kind: kind,
		list: tui.NewList(title, subtitle, items, searchable),
	}
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
	if model == nil || model.list == nil {
		return []Item{}
	}

	return model.list.Items()
}

// FilteredItems returns a copy of the visible item list.
func (model *Model) FilteredItems() []Item {
	if model == nil || model.list == nil {
		return []Item{}
	}

	return model.list.FilteredItems()
}

// SelectedIndex returns the currently selected visible row.
func (model *Model) SelectedIndex() int {
	if model == nil || model.list == nil {
		return 0
	}

	return model.list.SelectedIndex()
}

// SetSelectedIndex updates the selected visible row, clamping to the valid range.
func (model *Model) SetSelectedIndex(index int) {
	if model == nil || model.list == nil {
		return
	}

	model.list.SetSelectedIndex(index)
}

// SelectedValue returns the selected item's value.
func (model *Model) SelectedValue() (string, bool) {
	if model == nil || model.list == nil {
		return "", false
	}

	return model.list.SelectedValue()
}

// SelectedItem returns the selected item.
func (model *Model) SelectedItem() (Item, bool) {
	if model == nil || model.list == nil {
		return Item{Value: "", Title: "", Description: "", Meta: ""}, false
	}

	return model.list.SelectedItem()
}

// MoveSelection moves the selected row by delta, wrapping around the visible list.
func (model *Model) MoveSelection(delta int) {
	if model == nil || model.list == nil {
		return
	}

	model.list.MoveSelection(delta)
}

// AppendQueryRune appends a searchable rune to the panel query.
func (model *Model) AppendQueryRune(char rune) {
	if model == nil || model.list == nil {
		return
	}

	model.list.AppendQueryRune(char)
}

// BackspaceQuery removes one rune from the query.
func (model *Model) BackspaceQuery() {
	if model == nil || model.list == nil {
		return
	}

	model.list.BackspaceQuery()
}

// ApplyFilter refreshes visible items from the current query.
func (model *Model) ApplyFilter() {
	if model == nil || model.list == nil {
		return
	}

	model.list.ApplyFilter()
}

// Render returns styled terminal lines for the current panel state.
func (model *Model) Render(options *RenderOptions) []tui.Line {
	if model == nil || model.list == nil {
		return []tui.Line{}
	}

	return model.list.Render(options)
}
