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

// Model is a selectable app panel backed by a reusable TUI list.
type Model struct {
	*tui.List
	Kind Kind
}

// New returns a selection panel model.
func New(kind Kind, title, subtitle string, items []Item, searchable bool) *Model {
	return &Model{
		List: tui.NewList(title, subtitle, items, searchable),
		Kind: kind,
	}
}
