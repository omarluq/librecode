package extui

import (
	"maps"
	"slices"

	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/mapsutil"
	"github.com/omarluq/librecode/internal/terminal/input"
)

// Reserved buffer names exposed to terminal extensions.
const (
	BufferComposer   = "composer"
	BufferStatus     = "status"
	BufferTranscript = "transcript"
	BufferThinking   = "thinking"
	BufferTools      = "tools"
)

// Shared metadata keys for extension-visible runtime buffers.
const (
	MetadataCount   = "count"
	MetadataMessage = "message"
)

// Layout is the terminal's merged runtime layout for built-in and extension windows.
type Layout struct {
	Windows      map[string]extension.WindowState
	Transcript   extension.WindowState
	Autocomplete extension.WindowState
	Composer     extension.WindowState
	Status       extension.WindowState
	Width        int
	Height       int
}

// WindowOverride stores extension-driven draw operations for a single window.
type WindowOverride struct {
	DrawOps []extension.UIDrawOp
	Reset   bool
}

// State owns extension-driven terminal UI state.
type State struct {
	Buffers   map[string]extension.BufferState
	Windows   map[string]extension.WindowState
	Layout    *extension.LayoutState
	Overrides map[string]WindowOverride
	Cursor    *extension.UICursor
}

// NewState creates empty extension UI bridge state.
func NewState() State {
	return State{
		Buffers:   map[string]extension.BufferState{},
		Windows:   map[string]extension.WindowState{},
		Layout:    nil,
		Overrides: map[string]WindowOverride{},
		Cursor:    nil,
	}
}

// ResetFrameOverrides clears per-frame UI overrides.
func (state *State) ResetFrameOverrides() {
	state.Overrides = map[string]WindowOverride{}
	state.Cursor = nil
}

// RuntimeBuffer returns a cloned extension runtime buffer.
func (state *State) RuntimeBuffer(name string) (extension.BufferState, bool) {
	buffer, ok := state.Buffers[name]
	if !ok {
		var empty extension.BufferState

		return empty, false
	}

	return CloneBuffer(name, &buffer), true
}

// ApplyBuffer stores a cloned runtime buffer.
func (state *State) ApplyBuffer(name string, buffer *extension.BufferState) {
	state.Buffers[name] = CloneBuffer(name, buffer)
}

// DeleteBuffer removes a runtime buffer.
func (state *State) DeleteBuffer(name string) {
	delete(state.Buffers, name)
}

// ApplyWindow stores a runtime window and mirrors it into the active layout.
func (state *State) ApplyWindow(name string, window *extension.WindowState) {
	if window == nil {
		return
	}
	if window.Name == "" {
		window.Name = name
	}
	state.Windows[name] = *window
	state.ensureLayoutWindow(name, window)
}

// ApplyLayout stores a complete runtime layout snapshot.
func (state *State) ApplyLayout(layout *extension.LayoutState) {
	if layout == nil {
		return
	}
	cloned := extension.LayoutState{
		Windows: map[string]extension.WindowState{},
		Width:   layout.Width,
		Height:  layout.Height,
	}
	for name := range layout.Windows {
		window := layout.Windows[name]
		if window.Name == "" {
			window.Name = name
		}
		if window.Metadata == nil {
			window.Metadata = map[string]any{}
		}
		cloned.Windows[name] = window
	}
	state.Layout = &cloned
	state.Windows = map[string]extension.WindowState{}
	maps.Copy(state.Windows, cloned.Windows)
}

// DeleteWindow removes a runtime window, its overrides, and any cursor targeting it.
func (state *State) DeleteWindow(name string) {
	delete(state.Windows, name)
	if state.Layout != nil {
		delete(state.Layout.Windows, name)
	}
	delete(state.Overrides, name)
	if state.Cursor != nil && state.Cursor.Window == name {
		state.Cursor = nil
	}
}

// ResetWindowOverride clears draw operations for a window and marks it for clearing.
func (state *State) ResetWindowOverride(name string) {
	if name == "" {
		return
	}
	override := state.Overrides[name]
	override.Reset = true
	override.DrawOps = nil
	state.Overrides[name] = override
	if state.Cursor != nil && state.Cursor.Window == name {
		state.Cursor = nil
	}
}

// AppendDrawOp records an extension draw operation.
func (state *State) AppendDrawOp(drawOp *extension.UIDrawOp) {
	if drawOp == nil || drawOp.Window == "" {
		return
	}
	override := state.Overrides[drawOp.Window]
	override.DrawOps = append(override.DrawOps, *drawOp)
	state.Overrides[drawOp.Window] = override
}

// SetCursor stores an extension cursor override.
func (state *State) SetCursor(cursor *extension.UICursor) {
	if cursor == nil {
		return
	}
	cloned := *cursor
	state.Cursor = &cloned
}

func (state *State) ensureLayoutWindow(name string, window *extension.WindowState) {
	if state.Layout == nil || window == nil {
		return
	}
	if state.Layout.Windows == nil {
		state.Layout.Windows = map[string]extension.WindowState{}
	}
	state.Layout.Windows[name] = *window
}

// CloneBuffer clones a runtime buffer and normalizes its name, metadata, chars, and cursor.
func CloneBuffer(name string, buffer *extension.BufferState) extension.BufferState {
	if buffer == nil {
		return extension.BufferState{
			Metadata: map[string]any{},
			Blocks:   []extension.BufferBlock{},
			Name:     name,
			Text:     "",
			Label:    "",
			Chars:    []string{},
			Cursor:   0,
		}
	}
	cloned := *buffer
	if cloned.Name == "" {
		cloned.Name = name
	}
	cloned.Metadata = mapsutil.CloneOrEmpty(cloned.Metadata)
	cloned.Blocks = slices.Clone(cloned.Blocks)
	cloned.Chars = slices.Clone(cloned.Chars)
	if len(cloned.Chars) == 0 && cloned.Text != "" {
		cloned.Chars = input.StringChars(cloned.Text)
	}
	cloned.Cursor = input.ClampCursor(cloned.Cursor, len([]rune(cloned.Text)))

	return cloned
}
