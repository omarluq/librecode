package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/samber/oops"
	"github.com/vingarcia/ksql"
)

type entryRow struct {
	ParentID                   *string `ksql:"parent_id"`
	ID                         string  `ksql:"id"`
	SessionID                  string  `ksql:"session_id"`
	EntryType                  string  `ksql:"entry_type"`
	Role                       string  `ksql:"role"`
	Content                    string  `ksql:"content"`
	Provider                   string  `ksql:"provider"`
	Model                      string  `ksql:"model"`
	CustomType                 string  `ksql:"custom_type"`
	DataJSON                   string  `ksql:"data_json"`
	Summary                    string  `ksql:"summary"`
	CreatedAt                  string  `ksql:"created_at"`
	ToolName                   string  `ksql:"tool_name"`
	ToolStatus                 string  `ksql:"tool_status"`
	ToolArgsJSON               string  `ksql:"tool_args_json"`
	CompactionFirstKeptEntryID string  `ksql:"compaction_first_kept_entry_id"`
	BranchFromEntryID          string  `ksql:"branch_from_entry_id"`
	TokenEstimate              int     `ksql:"token_estimate"`
	ModelFacing                int     `ksql:"model_facing"`
	Display                    int     `ksql:"display"`
	CompactionTokensBefore     int     `ksql:"compaction_tokens_before"`
}

func entryFromRow(row *entryRow) (*EntryEntity, error) {
	createdAt, err := parseTime(row.CreatedAt)
	if err != nil {
		return nil, err
	}

	if row.ParentID != nil && *row.ParentID == "" {
		row.ParentID = nil
	}

	return &EntryEntity{
		CreatedAt: createdAt,
		ParentID:  row.ParentID,
		Message: MessageEntity{
			Timestamp: createdAt,
			Role:      Role(row.Role),
			Content:   row.Content,
			Provider:  row.Provider,
			Model:     row.Model,
		},
		Summary:                    row.Summary,
		ToolStatus:                 row.ToolStatus,
		Type:                       EntryType(row.EntryType),
		CustomType:                 row.CustomType,
		DataJSON:                   row.DataJSON,
		ID:                         row.ID,
		ToolName:                   row.ToolName,
		SessionID:                  row.SessionID,
		ToolArgsJSON:               row.ToolArgsJSON,
		BranchFromEntryID:          row.BranchFromEntryID,
		CompactionFirstKeptEntryID: row.CompactionFirstKeptEntryID,
		CompactionTokensBefore:     row.CompactionTokensBefore,
		TokenEstimate:              row.TokenEstimate,
		Display:                    row.Display != 0,
		ModelFacing:                row.ModelFacing != 0,
	}, nil
}

func entriesFromRows(rows []entryRow) ([]EntryEntity, error) {
	return collectSQLRows(rows, entryFromRow)
}

const entrySelectColumns = `
id, session_id, parent_id, entry_type, role, content,
provider, model, custom_type, data_json, summary, created_at,
tool_name, tool_status, tool_args_json, token_estimate, model_facing, display,
compaction_first_kept_entry_id, compaction_tokens_before, branch_from_entry_id`

// LeafEntry returns the newest appended entry for a session.
func (repository *SessionRepository) LeafEntry(ctx context.Context, sessionID string) (*EntryEntity, bool, error) {
	query := fmt.Sprintf(`
SELECT %s
FROM session_entries
WHERE session_id = ?
ORDER BY created_at DESC
LIMIT 1`, entrySelectColumns)

	var row entryRow
	if err := repository.sql.QueryOne(ctx, &row, query, sessionID); err != nil {
		if errors.Is(err, ksql.ErrRecordNotFound) {
			return nil, false, nil
		}

		return nil, false, oops.In("database").Code("leaf_entry").Wrapf(err, "load leaf entry")
	}

	entry, err := entryFromRow(&row)
	if err != nil {
		return nil, false, oops.In("database").Code("scan_entry").Wrapf(err, "scan leaf entry")
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

	rows := []entryRow{}
	if err := repository.sql.Query(ctx, &rows, query, sessionID); err != nil {
		return nil, oops.In("database").Code("list_entries").Wrapf(err, "query session entries")
	}

	entries, err := entriesFromRows(rows)
	if err != nil {
		return nil, oops.In("database").Code("scan_entry").Wrapf(err, "scan session entries")
	}

	return entries, nil
}

// Entry loads one entry by id.
func (repository *SessionRepository) Entry(ctx context.Context, sessionID, entryID string) (*EntryEntity, bool, error) {
	query := fmt.Sprintf(`
SELECT %s
FROM session_entries
WHERE session_id = ? AND id = ?`, entrySelectColumns)

	var row entryRow
	if err := repository.sql.QueryOne(ctx, &row, query, sessionID, entryID); err != nil {
		if errors.Is(err, ksql.ErrRecordNotFound) {
			return nil, false, nil
		}

		return nil, false, oops.In("database").Code("get_entry").Wrapf(err, "load entry")
	}

	entry, err := entryFromRow(&row)
	if err != nil {
		return nil, false, oops.In("database").Code("scan_entry").Wrapf(err, "scan entry")
	}

	return entry, true, nil
}

// DeleteEntryBranch removes an entry and all descendants from one session.
func (repository *SessionRepository) DeleteEntryBranch(ctx context.Context, sessionID, entryID string) error {
	if err := repository.sql.Transaction(ctx, func(transaction ksql.Provider) error {
		return repository.deleteEntryBranchTx(ctx, transaction, sessionID, entryID)
	}); err != nil {
		return oops.In("database").Code("delete_entry_branch").Wrapf(err, "delete entry branch")
	}

	return nil
}

func (repository *SessionRepository) deleteEntryBranchTx(
	ctx context.Context,
	transaction ksql.Provider,
	sessionID string,
	entryID string,
) error {
	if _, err := transaction.Exec(
		ctx,
		deleteEntryBranchMessages,
		sessionID,
		entryID,
		sessionID,
		sessionID,
	); err != nil {
		return oops.In("database").Code("delete_branch_messages").Wrapf(err, "delete branch messages")
	}

	if _, err := transaction.Exec(
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
	if _, err := transaction.Exec(ctx, touchSession, formatTime(repository.now().UTC()), sessionID); err != nil {
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

	rows := []entryRow{}
	if err := repository.sql.Query(ctx, &rows, query, args...); err != nil {
		return nil, oops.In("database").Code("list_children").Wrapf(err, "query child entries")
	}

	entries, err := entriesFromRows(rows)
	if err != nil {
		return nil, oops.In("database").Code("scan_child").Wrapf(err, "scan child entries")
	}

	return entries, nil
}

func (repository *SessionRepository) appendEntry(ctx context.Context, entry *EntryEntity) error {
	if err := repository.prepareAppendEntry(entry); err != nil {
		return err
	}

	if err := repository.sql.Transaction(ctx, func(transaction ksql.Provider) error {
		return repository.appendEntryTx(ctx, transaction, entry)
	}); err != nil {
		return oops.In("database").Code("append_entry_tx").Wrapf(err, "append entry transaction")
	}

	return nil
}

func (repository *SessionRepository) appendEntryTx(
	ctx context.Context,
	transaction ksql.Provider,
	entry *EntryEntity,
) error {
	if err := repository.insertEntryTx(ctx, transaction, entry); err != nil {
		return err
	}

	if err := repository.appendEntryMessage(ctx, transaction, entry); err != nil {
		return err
	}

	const touchSession = `UPDATE sessions SET updated_at = ? WHERE id = ?`
	if _, err := transaction.Exec(ctx, touchSession, formatTime(entry.CreatedAt), entry.SessionID); err != nil {
		return oops.In("database").Code("touch_session").Wrapf(err, "touch session")
	}

	return nil
}

func (repository *SessionRepository) prepareAppendEntry(entry *EntryEntity) error {
	if err := applyEntryMetadata(entry); err != nil {
		return oops.In("database").Code("entry_metadata").Wrapf(err, "prepare entry metadata")
	}

	if err := validateEntryEntity(entry); err != nil {
		return oops.In("database").Code("validate_entry").Wrapf(err, "validate entry")
	}

	return nil
}

func (repository *SessionRepository) insertEntryTx(
	ctx context.Context,
	transaction ksql.Provider,
	entry *EntryEntity,
) error {
	const insertEntry = `
INSERT INTO session_entries (
    id, session_id, parent_id, entry_type, role, content,
    provider, model, custom_type, data_json, summary, created_at,
    tool_name, tool_status, tool_args_json, token_estimate, model_facing, display,
    compaction_first_kept_entry_id, compaction_tokens_before, branch_from_entry_id
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := transaction.Exec(
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
		entry.ToolName,
		entry.ToolStatus,
		entry.ToolArgsJSON,
		entry.TokenEstimate,
		boolToInt(entry.ModelFacing),
		boolToInt(entry.Display),
		entry.CompactionFirstKeptEntryID,
		entry.CompactionTokensBefore,
		entry.BranchFromEntryID,
	)
	if err != nil {
		return oops.In("database").Code("append_entry").Wrapf(err, "append session entry")
	}

	return nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}

	return 0
}

func newEntryID() string {
	return newUUIDv7()
}

func normalizeDataJSON(dataJSON string) string {
	if dataJSON == "" {
		return "{}"
	}

	return dataJSON
}
