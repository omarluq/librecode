package llm

import "github.com/omarluq/librecode/internal/tool"

// ToolDefinition describes a callable model tool.
type ToolDefinition struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Schema      tool.Schema `json:"schema"`
	ReadOnly    bool        `json:"read_only,omitempty"`
}

// ToolCall is a provider-neutral request to invoke a tool.
type ToolCall struct {
	Metadata      map[string]any `json:"metadata,omitempty"`
	ArgumentsJSON string         `json:"arguments_json,omitempty"`
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Arguments     tool.Arguments `json:"arguments,omitzero"`
}

// ToolResult is a provider-neutral result for a tool invocation.
type ToolResult struct {
	Metadata      map[string]any `json:"metadata,omitempty"`
	ToolCallID    string         `json:"tool_call_id"`
	ArgumentsJSON string         `json:"arguments_json,omitempty"`
	Name          string         `json:"name,omitempty"`
	Error         string         `json:"error,omitempty"`
	Content       []Part         `json:"content,omitempty"`
	IsError       bool           `json:"is_error,omitempty"`
}
