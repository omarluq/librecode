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

// LeafEntry returns the newest appended entry for a session.
func (repository *SessionRepository) LeafEntry(ctx context.Context, sessionID string) (*EntryEntity, bool, error) {
	query := fmt.Sprintf(`
SELECT %s
FROM session_entries
WHERE session_id = ?
ORDER BY created_at DESC
LIMIT 1`, entrySelectColumns)

	entry, err := scanEntry(repository.connection.QueryRowContext(ctx, query, sessionID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, oops.In("database").Code("leaf_entry").Wrapf(err, "load leaf entry")
	}

	return entry, true, nil
}

// Entries returns all entries for a session in append order.
func (repository *SessionRepository) Entries(ctx context.Context, sessionID string) ([]EntryEntity, error) {
	query := fmt.Sprintf(`
SELECT %s
FROM session_entries
WHERE session_id = ?
ORDER BY created_at ASC`, entrySelectColumns)

	rows, err := repository.connection.QueryContext(ctx, query, sessionID)
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

// Entry loads one entry by id.
func (repository *SessionRepository) Entry(ctx context.Context, sessionID, entryID string) (*EntryEntity, bool, error) {
	query := fmt.Sprintf(`
SELECT %s
FROM session_entries
WHERE session_id = ? AND id = ?`, entrySelectColumns)

	entry, err := scanEntry(repository.connection.QueryRowContext(ctx, query, sessionID, entryID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, oops.In("database").Code("get_entry").Wrapf(err, "load entry")
	}

	return entry, true, nil
}

// DeleteEntryBranch removes an entry and all descendants from one session.
func (repository *SessionRepository) DeleteEntryBranch(ctx context.Context, sessionID, entryID string) error {
	transaction, err := repository.connection.BeginTx(ctx, nil)
	if err != nil {
		return oops.In("database").Code("begin_delete_entry_branch").Wrapf(err, "begin delete entry branch")
	}
	if err := repository.deleteEntryBranchTx(ctx, transaction, sessionID, entryID); err != nil {
		rollbackErr := transaction.Rollback()
		if rollbackErr != nil {
			return oops.
				In("database").
				Code("delete_entry_branch_rollback").
				Wrapf(rollbackErr, "rollback delete entry branch")
		}

		return err
	}
	if err := transaction.Commit(); err != nil {
		return oops.In("database").Code("commit_delete_entry_branch").Wrapf(err, "commit delete entry branch")
	}

	return nil
}

func (repository *SessionRepository) deleteEntryBranchTx(
	ctx context.Context,
	transaction *sql.Tx,
	sessionID string,
	entryID string,
) error {
	if _, err := transaction.ExecContext(
		ctx,
		deleteEntryBranchMessages,
		sessionID,
		entryID,
		sessionID,
		sessionID,
	); err != nil {
		return oops.In("database").Code("delete_branch_messages").Wrapf(err, "delete branch messages")
	}
	if _, err := transaction.ExecContext(
		ctx,
		deleteEntryBranchEntries,
		sessionID,
		entryID,
		sessionID,
		sessionID,
	); err != nil {
		return oops.In("database").Code("delete_branch_entries").Wrapf(err, "delete branch entries")
	}
	const touchSession = `UPDATE sessions SET updated_at = ? WHERE id = ?`
	if _, err := transaction.ExecContext(ctx, touchSession, formatTime(repository.now().UTC()), sessionID); err != nil {
		return oops.In("database").Code("touch_after_delete_branch").Wrapf(err, "touch session after delete branch")
	}

	return nil
}

const deleteEntryBranchMessages = `
WITH RECURSIVE subtree(id) AS (
    SELECT id FROM session_entries WHERE session_id = ? AND id = ?
    UNION ALL
    SELECT child.id
    FROM session_entries child
    JOIN subtree parent ON child.parent_id = parent.id
    WHERE child.session_id = ?
)
DELETE FROM session_messages
WHERE session_id = ? AND entry_id IN (SELECT id FROM subtree)`

const deleteEntryBranchEntries = `
WITH RECURSIVE subtree(id) AS (
    SELECT id FROM session_entries WHERE session_id = ? AND id = ?
    UNION ALL
    SELECT child.id
    FROM session_entries child
    JOIN subtree parent ON child.parent_id = parent.id
    WHERE child.session_id = ?
)
DELETE FROM session_entries
WHERE session_id = ? AND id IN (SELECT id FROM subtree)`

// Children returns direct child entries for a parent id.
func (repository *SessionRepository) Children(
	ctx context.Context,
	sessionID string,
	parentID *string,
) ([]EntryEntity, error) {
	query := fmt.Sprintf(`
SELECT %s
FROM session_entries
WHERE session_id = ? AND parent_id IS NULL
ORDER BY created_at ASC`, entrySelectColumns)
	args := []any{sessionID}
	if parentID != nil {
		query = fmt.Sprintf(`
SELECT %s
FROM session_entries
WHERE session_id = ? AND parent_id = ?
ORDER BY created_at ASC`, entrySelectColumns)
		args = append(args, *parentID)
	}

	rows, err := repository.connection.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, oops.In("database").Code("list_children").Wrapf(err, "query child entries")
	}

	return collectRows(rows, scanEntry, &rowErrorInfo{
		scanCode:  "scan_child",
		scanMsg:   "scan child entry",
		iterCode:  "iterate_children",
		iterMsg:   "iterate child entries",
		closeCode: "close_children",
		closeMsg:  "close child rows",
	})
}

func (repository *SessionRepository) appendEntry(ctx context.Context, entry *EntryEntity) error {
	if err := validateEntryEntity(entry); err != nil {
		return oops.In("database").Code("validate_entry").Wrapf(err, "validate entry")
	}

	const insertEntry = `
INSERT INTO session_entries (
    id, session_id, parent_id, entry_type, role, content,
    provider, model, custom_type, data_json, summary, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	transaction, err := repository.connection.BeginTx(ctx, nil)
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

	if err := repository.appendEntryMessage(ctx, transaction, entry); err != nil {
		rollbackErr := transaction.Rollback()
		if rollbackErr != nil {
			return oops.In("database").Code("append_entry_rollback").Wrapf(rollbackErr, "rollback append entry")
		}
		return err
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
