package llm

// ToolDefinition describes a callable model tool.
type ToolDefinition struct {
	Schema      map[string]any `json:"schema"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	ReadOnly    bool           `json:"read_only,omitempty"`
}

// ToolCall is a provider-neutral request to invoke a tool.
type ToolCall struct {
	Metadata      map[string]any `json:"metadata,omitempty"`
	Arguments     map[string]any `json:"arguments,omitempty"`
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	ArgumentsJSON string         `json:"arguments_json,omitempty"`
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
