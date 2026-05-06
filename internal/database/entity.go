package database

import "time"

// EntryType identifies a record in a session tree.
type EntryType string

const (
	// EntryTypeMessage stores a user, assistant, or tool message.
	EntryTypeMessage EntryType = "message"
	// EntryTypeCustom stores extension state that is not sent to a model.
	EntryTypeCustom EntryType = "custom"
	// EntryTypeCustomMessage stores extension context that participates in prompts.
	EntryTypeCustomMessage EntryType = "custom_message"
	// EntryTypeCompaction stores a context compaction summary.
	EntryTypeCompaction EntryType = "compaction"
)

// Role identifies the message author or payload category.
type Role string

const (
	// RoleUser is a user-authored prompt.
	RoleUser Role = "user"
	// RoleAssistant is an assistant response.
	RoleAssistant Role = "assistant"
	// RoleToolResult is output from a tool execution.
	RoleToolResult Role = "tool_result"
	// RoleCustom is extension-provided context.
	RoleCustom Role = "custom"
)

// MessageEntity is the durable representation of an assistant message.
type MessageEntity struct {
	Timestamp time.Time `json:"timestamp"`
	Role      Role      `json:"role"`
	Content   string    `json:"content"`
	Provider  string    `json:"provider,omitempty"`
	Model     string    `json:"model,omitempty"`
}

// SessionEntity is a persisted conversation root.
type SessionEntity struct {
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	ID            string    `json:"id"`
	CWD           string    `json:"cwd"`
	Name          string    `json:"name,omitempty"`
	ParentSession string    `json:"parent_session,omitempty"`
}

// EntryEntity is a persisted node in a session tree.
type EntryEntity struct {
	Message    MessageEntity `json:"message"`
	CreatedAt  time.Time     `json:"created_at"`
	ParentID   *string       `json:"parent_id,omitempty"`
	ID         string        `json:"id"`
	SessionID  string        `json:"session_id"`
	Type       EntryType     `json:"type"`
	CustomType string        `json:"custom_type,omitempty"`
	DataJSON   string        `json:"data_json,omitempty"`
	Summary    string        `json:"summary,omitempty"`
}

// KSQLRequestEntity is the JSON body posted to the ksqlDB REST API.
type KSQLRequestEntity struct {
	StreamsProperties map[string]string `json:"streamsProperties"`
	KSQL              string            `json:"ksql"`
}
