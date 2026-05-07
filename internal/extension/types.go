// Package extension loads and executes user workflow extensions.
package extension

import "context"

// Command describes a Lua slash command.
type Command struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Extension   string `json:"extension"`
}

// Tool describes a Lua-provided tool callable by the runtime.
type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Extension   string `json:"extension"`
}

// ToolResult is returned from Lua tool handlers.
type ToolResult struct {
	Details map[string]any `json:"details"`
	Content string         `json:"content"`
}

// LoadedExtension contains metadata for a loaded Lua source file.
type LoadedExtension struct {
	Name     string   `json:"name"`
	Path     string   `json:"path"`
	Commands []string `json:"commands"`
	Tools    []string `json:"tools"`
	Keymaps  []string `json:"keymaps"`
}

// BufferState describes an extension-visible mutable runtime buffer.
type BufferState struct {
	Metadata map[string]any `json:"metadata"`
	Name     string         `json:"name"`
	Text     string         `json:"text"`
	Label    string         `json:"label"`
	Chars    []string       `json:"chars"`
	Cursor   int            `json:"cursor"`
}

// BufferAppend records an append operation against a runtime buffer.
type BufferAppend struct {
	Name string `json:"name"`
	Text string `json:"text"`
	Role string `json:"role"`
}

// ActionCall requests a host-side runtime action.
type ActionCall struct {
	Name string `json:"name"`
}

// WindowState describes an extension-visible window or viewport.
type WindowState struct {
	Metadata  map[string]any `json:"metadata"`
	Name      string         `json:"name"`
	Role      string         `json:"role"`
	Buffer    string         `json:"buffer"`
	X         int            `json:"x"`
	Y         int            `json:"y"`
	Width     int            `json:"width"`
	Height    int            `json:"height"`
	CursorRow int            `json:"cursor_row"`
	CursorCol int            `json:"cursor_col"`
	Visible   bool           `json:"visible"`
}

// TerminalEvent describes a low-level terminal runtime event exposed to extensions.
type TerminalEvent struct {
	Buffers map[string]BufferState `json:"buffers"`
	Windows map[string]WindowState `json:"windows"`
	Context map[string]any         `json:"context"`
	Name    string                 `json:"name"`
	Key     ComposerKeyEvent       `json:"key"`
}

// TerminalEventResult describes mutations produced by low-level extension handlers.
type TerminalEventResult struct {
	Buffers        map[string]BufferState `json:"buffers"`
	Windows        map[string]WindowState `json:"windows"`
	Appends        []BufferAppend         `json:"appends"`
	Actions        []ActionCall           `json:"actions"`
	DeletedBuffers []string               `json:"deleted_buffers"`
	Consumed       bool                   `json:"consumed"`
}

// ComposerKeyEvent describes a terminal key event passed to a composer extension.
type ComposerKeyEvent struct {
	Key   string `json:"key"`
	Text  string `json:"text"`
	Ctrl  bool   `json:"ctrl"`
	Alt   bool   `json:"alt"`
	Shift bool   `json:"shift"`
}

// CommandRunner executes a named extension command.
type CommandRunner interface {
	ExecuteCommand(ctx context.Context, name, args string) (string, error)
}

// TerminalEventRunner executes low-level terminal runtime event handlers.
type TerminalEventRunner interface {
	HandleTerminalEvent(ctx context.Context, event *TerminalEvent) (TerminalEventResult, error)
}

// EventEmitter emits extension lifecycle events.
type EventEmitter interface {
	Emit(ctx context.Context, eventName string, payload map[string]any) error
}
