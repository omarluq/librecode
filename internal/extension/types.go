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
}

// CommandRunner executes a named extension command.
type CommandRunner interface {
	ExecuteCommand(ctx context.Context, name, args string) (string, error)
}

// EventEmitter emits extension lifecycle events.
type EventEmitter interface {
	Emit(ctx context.Context, eventName string, payload map[string]any) error
}
