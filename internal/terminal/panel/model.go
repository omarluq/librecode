// Package panel adapts the reusable TUI list component to app-specific panel kinds.
package panel

import "github.com/omarluq/librecode/internal/tui"

// Kind identifies the app-specific panel using this generic model.
type Kind string

// Model is a selectable app panel backed by a reusable TUI list.
type Model struct {
	*tui.List
	Kind Kind
}

// New returns a selection panel model.
func New(kind Kind, title, subtitle string, items []tui.ListItem, searchable bool) *Model {
	return &Model{
		List: tui.NewList(title, subtitle, items, searchable),
		Kind: kind,
	}
}
