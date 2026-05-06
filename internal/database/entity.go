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
	// EntryTypeBranchSummary stores context from an abandoned branch.
	EntryTypeBranchSummary EntryType = "branch_summary"
	// EntryTypeLabel stores a user-defined label for another entry.
	EntryTypeLabel EntryType = "label"
	// EntryTypeModelChange stores provider/model selection changes.
	EntryTypeModelChange EntryType = "model_change"
	// EntryTypeSessionInfo stores mutable session metadata such as display name.
	EntryTypeSessionInfo EntryType = "session_info"
	// EntryTypeThinkingLevelChange stores reasoning/thinking level changes.
	EntryTypeThinkingLevelChange EntryType = "thinking_level_change"
)

// Role identifies the message author or payload category.
type Role string

const (
	// RoleUser is a user-authored prompt.
	RoleUser Role = "user"
	// RoleAssistant is an assistant response.
	RoleAssistant Role = "assistant"
	// RoleToolResult is output from a tool execution.
	RoleToolResult Role = "toolResult"
	// RoleThinking is model reasoning or thinking text.
	RoleThinking Role = "thinking"
	// RoleCustom is extension-provided context.
	RoleCustom Role = "custom"
	// RoleBashExecution is output from a user-run shell command.
	RoleBashExecution Role = "bashExecution"
	// RoleBranchSummary is summary context for an abandoned branch.
	RoleBranchSummary Role = "branchSummary"
	// RoleCompactionSummary is summary context for compacted history.
	RoleCompactionSummary Role = "compactionSummary"
)

// MessageEntity is the context-facing representation of an assistant message.
type MessageEntity struct {
	Timestamp time.Time `json:"timestamp"`
	Role      Role      `json:"role"`
	Content   string    `json:"content"`
	Provider  string    `json:"provider,omitempty"`
	Model     string    `json:"model,omitempty"`
}

// SessionMessageEntity is the normalized durable message related to a session and entry.
type SessionMessageEntity struct {
	CreatedAt time.Time `json:"created_at"`
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	EntryID   string    `json:"entry_id"`
	Sender    string    `json:"sender"`
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

// EntryDataEntity stores flexible per-entry metadata encoded in session_entries.data_json.
type EntryDataEntity struct {
	Details          map[string]any `json:"details,omitempty"`
	Display          *bool          `json:"display,omitempty"`
	Label            *string        `json:"label,omitempty"`
	FirstKeptEntryID string         `json:"firstKeptEntryId,omitempty"`
	FromID           string         `json:"fromId,omitempty"`
	Name             string         `json:"name,omitempty"`
	TargetID         string         `json:"targetId,omitempty"`
	ThinkingLevel    string         `json:"thinkingLevel,omitempty"`
	TokensBefore     int            `json:"tokensBefore,omitempty"`
	FromHook         bool           `json:"fromHook,omitempty"`
}

// TreeNodeEntity is an entry and its direct descendants.
type TreeNodeEntity struct {
	Entry    EntryEntity      `json:"entry"`
	Children []TreeNodeEntity `json:"children"`
}

// SessionContextEntity is the reconstructed context from a session branch.
type SessionContextEntity struct {
	Provider      string          `json:"provider,omitempty"`
	Model         string          `json:"model,omitempty"`
	ThinkingLevel string          `json:"thinking_level,omitempty"`
	Messages      []MessageEntity `json:"messages"`
}

// DocumentEntity stores JSON-backed runtime documents in SQLite.
type DocumentEntity struct {
	UpdatedAt time.Time `json:"updated_at"`
	Namespace string    `json:"namespace"`
	Key       string    `json:"key"`
	ValueJSON string    `json:"value_json"`
}

// KSQLRequestEntity is the JSON body posted to the ksqlDB REST API.
type KSQLRequestEntity struct {
	StreamsProperties map[string]string `json:"streamsProperties"`
	KSQL              string            `json:"ksql"`
}
