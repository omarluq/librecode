package llm

import "context"

// ToolExecutor executes model-requested tool calls outside provider wire clients.
type ToolExecutor func(context.Context, []ToolCall, func(*StreamChunk)) ([]ToolResult, error)

// ProviderRequestHook can inspect and conservatively mutate a provider wire request.
// It returns HookOutput by value so a hook cannot return a nil output object.
type ProviderRequestHook func(context.Context, *HookInput) (HookOutput, error)

// ProviderObserver observes provider attempts without mutating them.
type ProviderObserver func(context.Context, *HookInput)

// HookInput describes a provider wire request before it is sent.
type HookInput struct {
	ProviderOptions map[string]any    `json:"provider_options,omitempty"`
	Payload         map[string]any    `json:"payload,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
	SessionID       string            `json:"session_id,omitempty"`
	ThinkingLevel   string            `json:"thinking_level,omitempty"`
	Model           ModelRef          `json:"model"`
	Attempt         int               `json:"attempt"`
}

// HookOutput describes a provider wire request after hook mutation.
type HookOutput struct {
	Payload map[string]any    `json:"payload,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}
