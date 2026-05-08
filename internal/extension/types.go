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

// UIStyle describes minimal low-level styling for extension-driven draw operations.
type UIStyle struct {
	FG     string `json:"fg"`
	BG     string `json:"bg"`
	Bold   bool   `json:"bold"`
	Italic bool   `json:"italic"`
}

// UIDrawOp describes one low-level window-relative drawing operation.
type UIDrawOp struct {
	Window string  `json:"window"`
	Text   string  `json:"text"`
	Style  UIStyle `json:"style"`
	Row    int     `json:"row"`
	Col    int     `json:"col"`
	Clear  bool    `json:"clear"`
}

// UICursor requests a cursor position relative to a window.
type UICursor struct {
	Window string `json:"window"`
	Row    int    `json:"row"`
	Col    int    `json:"col"`
}

// LayoutState describes the extension-visible terminal layout.
type LayoutState struct {
	Windows map[string]WindowState `json:"windows"`
	Width   int                    `json:"width"`
	Height  int                    `json:"height"`
}

// WindowState describes an extension-visible window or viewport.
type WindowState struct {
	Metadata  map[string]any `json:"metadata"`
	Name      string         `json:"name"`
	Role      string         `json:"role"`
	Buffer    string         `json:"buffer"`
	Renderer  string         `json:"renderer"`
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
	Layout  LayoutState            `json:"layout"`
	Context map[string]any         `json:"context"`
	Name    string                 `json:"name"`
	Key     ComposerKeyEvent       `json:"key"`
}

// TerminalEventResult describes mutations produced by low-level extension handlers.
type TerminalEventResult struct {
	Buffers        map[string]BufferState `json:"buffers"`
	Windows        map[string]WindowState `json:"windows"`
	Layout         *LayoutState           `json:"layout,omitempty"`
	UICursor       *UICursor              `json:"ui_cursor,omitempty"`
	Appends        []BufferAppend         `json:"appends"`
	Actions        []ActionCall           `json:"actions"`
	UIDrawOps      []UIDrawOp             `json:"ui_draw_ops"`
	ResetUIWindows []string               `json:"reset_ui_windows"`
	DeletedBuffers []string               `json:"deleted_buffers"`
	DeletedWindows []string               `json:"deleted_windows"`
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
