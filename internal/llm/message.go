package llm

// Role identifies the speaker for a model-facing message.
type Role string

const (
	// RoleSystem is provider or application instruction text.
	RoleSystem Role = "system"
	// RoleUser is user-authored text or app-authored context shown to the model.
	RoleUser Role = "user"
	// RoleAssistant is assistant-authored output.
	RoleAssistant Role = "assistant"
	// RoleTool is a tool result message.
	RoleTool Role = "tool"
)

// PartType identifies a typed content block.
type PartType string

const (
	// PartText is ordinary natural-language text.
	PartText PartType = "text"
	// PartReasoning is provider reasoning or thinking content.
	PartReasoning PartType = "reasoning"
	// PartImage is an inline image content block.
	PartImage PartType = "image"
	// PartFile is a file/document content block.
	PartFile PartType = "file"
	// PartSource is a source citation content block.
	PartSource PartType = "source"
	// PartToolCall is a tool-call content block.
	PartToolCall PartType = "tool_call"
	// PartToolResult is a tool-result content block.
	PartToolResult PartType = "tool_result"
)

// Message is one provider-neutral model-facing message.
type Message struct {
	Metadata map[string]any `json:"metadata,omitempty"`
	Role     Role           `json:"role"`
	Content  []Part         `json:"content,omitempty"`
}

// Part is one typed content block inside a message or response.
// Image/document inputs and provider cache-control metadata are intentionally
// flattened for this staged boundary and can grow when runtime support lands.
type Part struct {
	Metadata   map[string]any `json:"metadata,omitempty"`
	ToolCall   *ToolCall      `json:"tool_call,omitempty"`
	ToolResult *ToolResult    `json:"tool_result,omitempty"`
	Type       PartType       `json:"type"`
	Text       string         `json:"text,omitempty"`
	Data       string         `json:"data,omitempty"`
	MIMEType   string         `json:"mime_type,omitempty"`
}

// TextMessage creates a message with one text part.
func TextMessage(role Role, text string) Message {
	return Message{
		Metadata: nil,
		Role:     role,
		Content: []Part{
			TextPart(text),
		},
	}
}

// TextPart creates one text part.
func TextPart(text string) Part {
	return Part{
		Metadata:   nil,
		ToolCall:   nil,
		ToolResult: nil,
		Type:       PartText,
		Text:       text,
		Data:       "",
		MIMEType:   "",
	}
}
