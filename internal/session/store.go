// Package session persists agent sessions as SQLite-backed trees.
package session

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/samber/oops"
)

// EntryType identifies a record in a session tree.
type EntryType string

const (
	// EntryTypeMessage stores a user, assistant, or tool message.
	EntryTypeMessage EntryType = "message"
	// EntryTypeCustom stores Lua plugin state that is not sent to a model.
	EntryTypeCustom EntryType = "custom"
	// EntryTypeCustomMessage stores Lua plugin context that participates in prompts.
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
	// RoleCustom is plugin-provided context.
	RoleCustom Role = "custom"
)

// Message is the durable representation of an agent message.
type Message struct {
	Role      Role      `json:"role"`
	Content   string    `json:"content"`
	Provider  string    `json:"provider,omitempty"`
	Model     string    `json:"model,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// Session is a persisted conversation root.
type Session struct {
	ID            string    `json:"id"`
	CWD           string    `json:"cwd"`
	Name          string    `json:"name,omitempty"`
	ParentSession string    `json:"parent_session,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Entry is a node in a session tree.
type Entry struct {
	ID         string    `json:"id"`
	SessionID  string    `json:"session_id"`
	ParentID   *string   `json:"parent_id,omitempty"`
	Type       EntryType `json:"type"`
	Message    Message   `json:"message"`
	CustomType string    `json:"custom_type,omitempty"`
	DataJSON   string    `json:"data_json,omitempty"`
	Summary    string    `json:"summary,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// Store provides SQLite persistence for sessions and tree entries.
type Store struct {
	database *sql.DB
	now      func() time.Time
}

// NewStore creates a SQLite session store.
func NewStore(database *sql.DB) *Store {
	return &Store{
		database: database,
		now:      time.Now,
	}
}

// CreateSession creates a new persisted session for a working directory.
func (store *Store) CreateSession(ctx context.Context, cwd string, name string, parentSession string) (*Session, error) {
	timestamp := store.now().UTC()
	session := Session{
		ID:            uuid.NewString(),
		CWD:           cwd,
		Name:          name,
		ParentSession: parentSession,
		CreatedAt:     timestamp,
		UpdatedAt:     timestamp,
	}

	const statement = `
INSERT INTO sessions (id, cwd, name, parent_session, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)`
	if _, err := store.database.ExecContext(
		ctx,
		statement,
		session.ID,
		session.CWD,
		session.Name,
		session.ParentSession,
		formatTime(session.CreatedAt),
		formatTime(session.UpdatedAt),
	); err != nil {
		return nil, oops.In("session").Code("create_session").Wrapf(err, "create session")
	}

	return &session, nil
}

// LatestSession returns the newest session for cwd.
func (store *Store) LatestSession(ctx context.Context, cwd string) (*Session, bool, error) {
	const query = `
SELECT id, cwd, name, parent_session, created_at, updated_at
FROM sessions
WHERE cwd = ?
ORDER BY updated_at DESC
LIMIT 1`

	session, err := scanSession(store.database.QueryRowContext(ctx, query, cwd))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, oops.In("session").Code("latest_session").Wrapf(err, "load latest session")
	}

	return session, true, nil
}

// GetSession loads a session by id.
func (store *Store) GetSession(ctx context.Context, sessionID string) (*Session, bool, error) {
	const query = `
SELECT id, cwd, name, parent_session, created_at, updated_at
FROM sessions
WHERE id = ?`

	session, err := scanSession(store.database.QueryRowContext(ctx, query, sessionID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, oops.In("session").Code("get_session").Wrapf(err, "load session")
	}

	return session, true, nil
}

// ListSessions returns sessions for cwd ordered by newest first.
func (store *Store) ListSessions(ctx context.Context, cwd string) ([]Session, error) {
	const query = `
SELECT id, cwd, name, parent_session, created_at, updated_at
FROM sessions
WHERE cwd = ?
ORDER BY updated_at DESC`

	rows, err := store.database.QueryContext(ctx, query, cwd)
	if err != nil {
		return nil, oops.In("session").Code("list_sessions").Wrapf(err, "query sessions")
	}
	defer rows.Close()

	sessions := []Session{}
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			return nil, oops.In("session").Code("scan_session").Wrapf(err, "scan session")
		}
		sessions = append(sessions, *session)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.In("session").Code("iterate_sessions").Wrapf(err, "iterate sessions")
	}

	return sessions, nil
}

// LeafEntry returns the newest appended entry for a session.
func (store *Store) LeafEntry(ctx context.Context, sessionID string) (*Entry, bool, error) {
	const query = `
SELECT id, session_id, parent_id, entry_type, role, content, provider, model, custom_type, data_json, summary, created_at
FROM session_entries
WHERE session_id = ?
ORDER BY created_at DESC
LIMIT 1`

	entry, err := scanEntry(store.database.QueryRowContext(ctx, query, sessionID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, oops.In("session").Code("leaf_entry").Wrapf(err, "load leaf entry")
	}

	return entry, true, nil
}

// Entries returns all entries for a session in append order.
func (store *Store) Entries(ctx context.Context, sessionID string) ([]Entry, error) {
	const query = `
SELECT id, session_id, parent_id, entry_type, role, content, provider, model, custom_type, data_json, summary, created_at
FROM session_entries
WHERE session_id = ?
ORDER BY created_at ASC`

	rows, err := store.database.QueryContext(ctx, query, sessionID)
	if err != nil {
		return nil, oops.In("session").Code("list_entries").Wrapf(err, "query session entries")
	}
	defer rows.Close()

	entries := []Entry{}
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return nil, oops.In("session").Code("scan_entry").Wrapf(err, "scan session entry")
		}
		entries = append(entries, *entry)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.In("session").Code("iterate_entries").Wrapf(err, "iterate session entries")
	}

	return entries, nil
}

// AppendMessage appends a message as a child of the current leaf or provided parent.
func (store *Store) AppendMessage(
	ctx context.Context,
	sessionID string,
	parentID *string,
	message Message,
) (*Entry, error) {
	entry := Entry{
		ID:         newEntryID(),
		SessionID:  sessionID,
		ParentID:   parentID,
		Type:       EntryTypeMessage,
		Message:    message,
		CustomType: "",
		DataJSON:   "{}",
		Summary:    "",
		CreatedAt:  store.now().UTC(),
	}

	if err := store.appendEntry(ctx, entry); err != nil {
		return nil, err
	}

	return &entry, nil
}

// AppendCustom appends plugin state that does not participate in prompt context.
func (store *Store) AppendCustom(ctx context.Context, sessionID string, customType string, dataJSON string) (*Entry, error) {
	entry := Entry{
		ID:        newEntryID(),
		SessionID: sessionID,
		ParentID:  nil,
		Type:      EntryTypeCustom,
		Message: Message{
			Role:      "",
			Content:   "",
			Provider:  "",
			Model:     "",
			Timestamp: store.now().UTC(),
		},
		CustomType: customType,
		DataJSON:   normalizeDataJSON(dataJSON),
		Summary:    "",
		CreatedAt:  store.now().UTC(),
	}

	if err := store.appendEntry(ctx, entry); err != nil {
		return nil, err
	}

	return &entry, nil
}

func (store *Store) appendEntry(ctx context.Context, entry Entry) error {
	const insertEntry = `
INSERT INTO session_entries (
    id, session_id, parent_id, entry_type, role, content, provider, model, custom_type, data_json, summary, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return oops.In("session").Code("begin_append").Wrapf(err, "begin append entry")
	}

	if _, err := transaction.ExecContext(
		ctx,
		insertEntry,
		entry.ID,
		entry.SessionID,
		entry.ParentID,
		string(entry.Type),
		string(entry.Message.Role),
		entry.Message.Content,
		entry.Message.Provider,
		entry.Message.Model,
		entry.CustomType,
		entry.DataJSON,
		entry.Summary,
		formatTime(entry.CreatedAt),
	); err != nil {
		rollbackErr := transaction.Rollback()
		if rollbackErr != nil {
			return oops.In("session").Code("append_entry_rollback").Wrapf(rollbackErr, "rollback append entry")
		}
		return oops.In("session").Code("append_entry").Wrapf(err, "append session entry")
	}

	const touchSession = `UPDATE sessions SET updated_at = ? WHERE id = ?`
	if _, err := transaction.ExecContext(ctx, touchSession, formatTime(entry.CreatedAt), entry.SessionID); err != nil {
		rollbackErr := transaction.Rollback()
		if rollbackErr != nil {
			return oops.In("session").Code("touch_session_rollback").Wrapf(rollbackErr, "rollback touch session")
		}
		return oops.In("session").Code("touch_session").Wrapf(err, "touch session")
	}

	if err := transaction.Commit(); err != nil {
		return oops.In("session").Code("commit_append").Wrapf(err, "commit append entry")
	}

	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSession(scanner rowScanner) (*Session, error) {
	var createdAt string
	var updatedAt string
	session := Session{
		ID:            "",
		CWD:           "",
		Name:          "",
		ParentSession: "",
		CreatedAt:     time.Time{},
		UpdatedAt:     time.Time{},
	}

	if err := scanner.Scan(
		&session.ID,
		&session.CWD,
		&session.Name,
		&session.ParentSession,
		&createdAt,
		&updatedAt,
	); err != nil {
		return nil, err
	}

	parsedCreatedAt, err := parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	parsedUpdatedAt, err := parseTime(updatedAt)
	if err != nil {
		return nil, err
	}
	session.CreatedAt = parsedCreatedAt
	session.UpdatedAt = parsedUpdatedAt

	return &session, nil
}

func scanEntry(scanner rowScanner) (*Entry, error) {
	var parentID sql.NullString
	var createdAt string
	var entryType string
	var role string
	entry := Entry{
		ID:        "",
		SessionID: "",
		ParentID:  nil,
		Type:      "",
		Message: Message{
			Role:      "",
			Content:   "",
			Provider:  "",
			Model:     "",
			Timestamp: time.Time{},
		},
		CustomType: "",
		DataJSON:   "",
		Summary:    "",
		CreatedAt:  time.Time{},
	}

	if err := scanner.Scan(
		&entry.ID,
		&entry.SessionID,
		&parentID,
		&entryType,
		&role,
		&entry.Message.Content,
		&entry.Message.Provider,
		&entry.Message.Model,
		&entry.CustomType,
		&entry.DataJSON,
		&entry.Summary,
		&createdAt,
	); err != nil {
		return nil, err
	}

	if parentID.Valid {
		entry.ParentID = &parentID.String
	}

	parsedCreatedAt, err := parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	entry.Type = EntryType(entryType)
	entry.Message.Role = Role(role)
	entry.Message.Timestamp = parsedCreatedAt
	entry.CreatedAt = parsedCreatedAt

	return &entry, nil
}

func newEntryID() string {
	buffer := make([]byte, 4)
	if _, err := rand.Read(buffer); err != nil {
		return uuid.NewString()[:8]
	}

	return hex.EncodeToString(buffer)
}

func normalizeDataJSON(dataJSON string) string {
	if dataJSON == "" {
		return "{}"
	}

	return dataJSON
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("session: parse timestamp: %w", err)
	}

	return parsed, nil
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
