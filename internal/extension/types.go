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
	Name          string   `json:"name"`
	Path          string   `json:"path"`
	Commands      []string `json:"commands"`
	Tools         []string `json:"tools"`
	ComposerModes []string `json:"composer_modes"`
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

// TerminalEvent describes a low-level terminal runtime event exposed to extensions.
type TerminalEvent struct {
	Buffers map[string]BufferState `json:"buffers"`
	Context map[string]any         `json:"context"`
	Name    string                 `json:"name"`
	Key     ComposerKeyEvent       `json:"key"`
}

// TerminalEventResult describes mutations produced by low-level extension handlers.
type TerminalEventResult struct {
	Buffers  map[string]BufferState `json:"buffers"`
	Appends  []BufferAppend         `json:"appends"`
	Consumed bool                   `json:"consumed"`
}

// ComposerMode describes an extension-provided terminal composer mode.
type ComposerMode struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Extension   string `json:"extension"`
	Label       string `json:"label"`
	Default     bool   `json:"default"`
}

// ComposerKeyEvent describes a terminal key event passed to a composer extension.
type ComposerKeyEvent struct {
	Key  string `json:"key"`
	Text string `json:"text"`
	Ctrl bool   `json:"ctrl"`
	Alt  bool   `json:"alt"`
}

// ComposerState describes the current chat composer editor state.
type ComposerState struct {
	Text        string   `json:"text"`
	Chars       []string `json:"chars"`
	Cursor      int      `json:"cursor"`
	Working     bool     `json:"working"`
	AuthWorking bool     `json:"auth_working"`
}

// ComposerResult describes mutations returned by a composer extension.
type ComposerResult struct {
	Text      string `json:"text"`
	Label     string `json:"label"`
	Cursor    int    `json:"cursor"`
	Handled   bool   `json:"handled"`
	HasText   bool   `json:"has_text"`
	HasCursor bool   `json:"has_cursor"`
}

// CommandRunner executes a named extension command.
type CommandRunner interface {
	ExecuteCommand(ctx context.Context, name, args string) (string, error)
}

// ComposerRunner executes extension-provided composer modes.
type ComposerRunner interface {
	HandleComposerKey(
		ctx context.Context,
		mode string,
		event ComposerKeyEvent,
		state ComposerState,
	) (ComposerResult, error)
}

// TerminalEventRunner executes low-level terminal runtime event handlers.
type TerminalEventRunner interface {
	HandleTerminalEvent(ctx context.Context, event TerminalEvent) (TerminalEventResult, error)
}

// EventEmitter emits extension lifecycle events.
type EventEmitter interface {
	Emit(ctx context.Context, eventName string, payload map[string]any) error
}
