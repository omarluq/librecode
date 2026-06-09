package llm

import "context"

// Generator generates model responses from provider-neutral requests.
type Generator interface {
	Generate(ctx context.Context, request *Request) (*Response, error)
}

// Streamer generates streamed model responses from provider-neutral requests.
type Streamer interface {
	Stream(ctx context.Context, request Request) (Stream, error)
}

// Stream yields provider-neutral response chunks.
type Stream interface {
	Recv() (*StreamChunk, error)
	Close() error
}

// Request describes one provider-neutral LLM generation call.
type Request struct {
	ProviderOptions map[string]any   `json:"provider_options,omitempty"`
	Auth            Auth             `json:"auth"`
	SystemPrompt    string           `json:"system_prompt,omitempty"`
	ThinkingLevel   string           `json:"thinking_level,omitempty"`
	SessionID       string           `json:"session_id,omitempty"`
	Messages        []Message        `json:"messages"`
	Tools           []ToolDefinition `json:"tools,omitempty"`
	Model           ModelRef         `json:"model"`
	Usage           Usage            `json:"usage"`
	DisableTools    bool             `json:"disable_tools,omitempty"`
}

// Response is a completed provider-neutral LLM response.
type Response struct {
	FinishReason FinishReason `json:"finish_reason,omitempty"`
	Content      []Part       `json:"content,omitempty"`
	ToolCalls    []ToolCall   `json:"tool_calls,omitempty"`
	Usage        Usage        `json:"usage"`
}

// StreamChunk is one provider-neutral streaming delta.
type StreamChunk struct {
	Part         *Part        `json:"part,omitempty"`
	ToolCall     *ToolCall    `json:"tool_call,omitempty"`
	FinishReason FinishReason `json:"finish_reason,omitempty"`
	Usage        Usage        `json:"usage"`
}

// Auth contains provider request credentials and extra headers.
type Auth struct {
	Headers map[string]string `json:"headers,omitempty"`
	APIKey  string            `json:"api_key,omitempty"`
}

// ModelRef identifies the concrete model and provider endpoint family.
type ModelRef struct {
	Metadata         map[string]any     `json:"metadata,omitempty"`
	ThinkingLevelMap map[string]*string `json:"thinking_level_map,omitempty"`
	Provider         string             `json:"provider"`
	ID               string             `json:"id"`
	API              string             `json:"api,omitempty"`
	BaseURL          string             `json:"base_url,omitempty"`
	MaxTokens        int                `json:"max_tokens,omitempty"`
	ContextWindow    int                `json:"context_window,omitempty"`
	Reasoning        bool               `json:"reasoning,omitempty"`
}
