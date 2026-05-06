// Package database contains database-backed persistence and adapters.
package database

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

const entrySelectColumns = `
id, session_id, parent_id, entry_type, role, content,
provider, model, custom_type, data_json, summary, created_at`

// SessionStore provides persistence for sessions and tree entries.
type SessionStore struct {
	connection *sql.DB
	now        func() time.Time
}

// NewSessionStore creates a session store.
func NewSessionStore(connection *sql.DB) *SessionStore {
	return &SessionStore{
		connection: connection,
		now:        time.Now,
	}
}

// CreateSession creates a new persisted session for a working directory.
func (store *SessionStore) CreateSession(ctx context.Context, cwd, name, parentSession string) (*SessionEntity, error) {
	timestamp := store.now().UTC()
	createdSession := SessionEntity{
		CreatedAt:     timestamp,
		UpdatedAt:     timestamp,
		ID:            uuid.NewString(),
		CWD:           cwd,
		Name:          name,
		ParentSession: parentSession,
	}

	const statement = `
INSERT INTO sessions (id, cwd, name, parent_session, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)`
	if _, err := store.connection.ExecContext(
		ctx,
		statement,
		createdSession.ID,
		createdSession.CWD,
		createdSession.Name,
		createdSession.ParentSession,
		formatTime(createdSession.CreatedAt),
		formatTime(createdSession.UpdatedAt),
	); err != nil {
		return nil, oops.In("database").Code("create_session").Wrapf(err, "create session")
	}

	return &createdSession, nil
}

// LatestSession returns the newest session for cwd.
func (store *SessionStore) LatestSession(ctx context.Context, cwd string) (*SessionEntity, bool, error) {
	const query = `
SELECT id, cwd, name, parent_session, created_at, updated_at
FROM sessions
WHERE cwd = ?
ORDER BY updated_at DESC
LIMIT 1`

	foundSession, err := scanSession(store.connection.QueryRowContext(ctx, query, cwd))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, oops.In("database").Code("latest_session").Wrapf(err, "load latest session")
	}

	return foundSession, true, nil
}

// GetSession loads a session by id.
func (store *SessionStore) GetSession(ctx context.Context, sessionID string) (*SessionEntity, bool, error) {
	const query = `
SELECT id, cwd, name, parent_session, created_at, updated_at
FROM sessions
WHERE id = ?`

	foundSession, err := scanSession(store.connection.QueryRowContext(ctx, query, sessionID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, oops.In("database").Code("get_session").Wrapf(err, "load session")
	}

	return foundSession, true, nil
}

// ListSessions returns sessions for cwd ordered by newest first.
func (store *SessionStore) ListSessions(ctx context.Context, cwd string) ([]SessionEntity, error) {
	const query = `
SELECT id, cwd, name, parent_session, created_at, updated_at
FROM sessions
WHERE cwd = ?
ORDER BY updated_at DESC`

	rows, err := store.connection.QueryContext(ctx, query, cwd)
	if err != nil {
		return nil, oops.In("database").Code("list_sessions").Wrapf(err, "query sessions")
	}

	return collectRows(rows, scanSession, &rowErrorInfo{
		scanCode:  "scan_session",
		scanMsg:   "scan session",
		iterCode:  "iterate_sessions",
		iterMsg:   "iterate sessions",
		closeCode: "close_sessions",
		closeMsg:  "close session rows",
	})
}

// LeafEntry returns the newest appended entry for a session.
func (store *SessionStore) LeafEntry(ctx context.Context, sessionID string) (*EntryEntity, bool, error) {
	query := fmt.Sprintf(`
SELECT %s
FROM session_entries
WHERE session_id = ?
ORDER BY created_at DESC
LIMIT 1`, entrySelectColumns)

	entry, err := scanEntry(store.connection.QueryRowContext(ctx, query, sessionID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, oops.In("database").Code("leaf_entry").Wrapf(err, "load leaf entry")
	}

	return entry, true, nil
}

// Entries returns all entries for a session in append order.
func (store *SessionStore) Entries(ctx context.Context, sessionID string) ([]EntryEntity, error) {
	query := fmt.Sprintf(`
SELECT %s
FROM session_entries
WHERE session_id = ?
ORDER BY created_at ASC`, entrySelectColumns)

	rows, err := store.connection.QueryContext(ctx, query, sessionID)
	if err != nil {
		return nil, oops.In("database").Code("list_entries").Wrapf(err, "query session entries")
	}

	return collectRows(rows, scanEntry, &rowErrorInfo{
		scanCode:  "scan_entry",
		scanMsg:   "scan session entry",
		iterCode:  "iterate_entries",
		iterMsg:   "iterate session entries",
		closeCode: "close_entries",
		closeMsg:  "close entry rows",
	})
}

// AppendMessage appends a message as a child of the current leaf or provided parent.
func (store *SessionStore) AppendMessage(
	ctx context.Context,
	sessionID string,
	parentID *string,
	message *MessageEntity,
) (*EntryEntity, error) {
	entry := EntryEntity{
		Message:    *message,
		CreatedAt:  store.now().UTC(),
		ParentID:   parentID,
		ID:         newEntryID(),
		SessionID:  sessionID,
		Type:       EntryTypeMessage,
		CustomType: "",
		DataJSON:   "{}",
		Summary:    "",
	}

	if err := store.appendEntry(ctx, &entry); err != nil {
		return nil, err
	}

	return &entry, nil
}

// AppendCustom appends extension state that does not participate in prompt context.
func (store *SessionStore) AppendCustom(
	ctx context.Context,
	sessionID string,
	customType string,
	dataJSON string,
) (*EntryEntity, error) {
	timestamp := store.now().UTC()
	entry := EntryEntity{
		Message: MessageEntity{
			Timestamp: timestamp,
			Role:      "",
			Content:   "",
			Provider:  "",
			Model:     "",
		},
		CreatedAt:  timestamp,
		ParentID:   nil,
		ID:         newEntryID(),
		SessionID:  sessionID,
		Type:       EntryTypeCustom,
		CustomType: customType,
		DataJSON:   normalizeDataJSON(dataJSON),
		Summary:    "",
	}

	if err := store.appendEntry(ctx, &entry); err != nil {
		return nil, err
	}

	return &entry, nil
}

func (store *SessionStore) appendEntry(ctx context.Context, entry *EntryEntity) error {
	const insertEntry = `
INSERT INTO session_entries (
    id, session_id, parent_id, entry_type, role, content,
    provider, model, custom_type, data_json, summary, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	transaction, err := store.connection.BeginTx(ctx, nil)
	if err != nil {
		return oops.In("database").Code("begin_append").Wrapf(err, "begin append entry")
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
			return oops.In("database").Code("append_entry_rollback").Wrapf(rollbackErr, "rollback append entry")
		}
		return oops.In("database").Code("append_entry").Wrapf(err, "append session entry")
	}

	const touchSession = `UPDATE sessions SET updated_at = ? WHERE id = ?`
	if _, err := transaction.ExecContext(ctx, touchSession, formatTime(entry.CreatedAt), entry.SessionID); err != nil {
		rollbackErr := transaction.Rollback()
		if rollbackErr != nil {
			return oops.In("database").Code("touch_session_rollback").Wrapf(rollbackErr, "rollback touch session")
		}
		return oops.In("database").Code("touch_session").Wrapf(err, "touch session")
	}

	if err := transaction.Commit(); err != nil {
		return oops.In("database").Code("commit_append").Wrapf(err, "commit append entry")
	}

	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

type rowErrorInfo struct {
	scanCode  string
	scanMsg   string
	iterCode  string
	iterMsg   string
	closeCode string
	closeMsg  string
}

func collectRows[T any](rows *sql.Rows, scan func(rowScanner) (*T, error), errorInfo *rowErrorInfo) (
	items []T,
	err error,
) {
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = oops.In("database").Code(errorInfo.closeCode).Wrapf(closeErr, "%s", errorInfo.closeMsg)
		}
	}()

	items = []T{}
	for rows.Next() {
		item, scanErr := scan(rows)
		if scanErr != nil {
			return nil, oops.In("database").Code(errorInfo.scanCode).Wrapf(scanErr, "%s", errorInfo.scanMsg)
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.In("database").Code(errorInfo.iterCode).Wrapf(err, "%s", errorInfo.iterMsg)
	}

	return items, nil
}

func scanSession(scanner rowScanner) (*SessionEntity, error) {
	var createdAt string
	var updatedAt string
	foundSession := SessionEntity{
		CreatedAt:     time.Time{},
		UpdatedAt:     time.Time{},
		ID:            "",
		CWD:           "",
		Name:          "",
		ParentSession: "",
	}

	if err := scanner.Scan(
		&foundSession.ID,
		&foundSession.CWD,
		&foundSession.Name,
		&foundSession.ParentSession,
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
	foundSession.CreatedAt = parsedCreatedAt
	foundSession.UpdatedAt = parsedUpdatedAt

	return &foundSession, nil
}

func scanEntry(scanner rowScanner) (*EntryEntity, error) {
	var parentID sql.NullString
	var createdAt string
	var entryType string
	var role string
	entry := EntryEntity{
		Message: MessageEntity{
			Timestamp: time.Time{},
			Role:      "",
			Content:   "",
			Provider:  "",
			Model:     "",
		},
		CreatedAt:  time.Time{},
		ParentID:   nil,
		ID:         "",
		SessionID:  "",
		Type:       "",
		CustomType: "",
		DataJSON:   "",
		Summary:    "",
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
		return time.Time{}, fmt.Errorf("database: parse timestamp: %w", err)
	}

	return parsed, nil
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
