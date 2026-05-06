// Package plugin loads and executes Lua plugins.
package plugin

import "context"

// Command describes a Lua slash command.
type Command struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Plugin      string `json:"plugin"`
}

// Tool describes a Lua-provided tool callable by the runtime.
type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Plugin      string `json:"plugin"`
}

// ToolResult is returned from Lua tool handlers.
type ToolResult struct {
	Content string         `json:"content"`
	Details map[string]any `json:"details"`
}

// LoadedPlugin contains metadata for a loaded Lua source file.
type LoadedPlugin struct {
	Name     string   `json:"name"`
	Path     string   `json:"path"`
	Commands []string `json:"commands"`
	Tools    []string `json:"tools"`
}

// CommandRunner executes a named plugin command.
type CommandRunner interface {
	ExecuteCommand(ctx context.Context, name string, args string) (string, error)
}

// EventEmitter emits plugin lifecycle events.
type EventEmitter interface {
	Emit(ctx context.Context, eventName string, payload map[string]any) error
}
