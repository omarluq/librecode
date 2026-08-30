package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/samber/oops"
	"github.com/vingarcia/ksql"
)

type entryRow struct {
	ParentID                   *string `ksql:"parent_id"`
	ID                         string  `ksql:"id"`
	SessionID                  string  `ksql:"session_id"`
	EntryType                  string  `ksql:"entry_type"`
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
			Role:      "",
			Content:   "",
			Provider:  "",
			Model:     "",
			Parts:     nil,
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
id, session_id, parent_id, entry_type,
custom_type, data_json, summary, created_at,
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

	return repository.queryEntry(ctx, query, "leaf_entry", "load leaf entry", sessionID)
}

// Entries returns all entries for a session in append order.
func (repository *SessionRepository) Entries(ctx context.Context, sessionID string) ([]EntryEntity, error) {
	query := fmt.Sprintf(`
SELECT %s
FROM session_entries
WHERE session_id = ?
ORDER BY created_at ASC`, entrySelectColumns)

	return repository.queryEntries(ctx, query, "list_entries", "scan_entry", "entries", sessionID)
}

// Entry loads one entry by id.
func (repository *SessionRepository) Entry(ctx context.Context, sessionID, entryID string) (*EntryEntity, bool, error) {
	query := fmt.Sprintf(`
SELECT %s
FROM session_entries
WHERE session_id = ? AND id = ?`, entrySelectColumns)

	return repository.queryEntry(ctx, query, "get_entry", "load entry", sessionID, entryID)
}

func (repository *SessionRepository) queryEntry(
	ctx context.Context,
	query string,
	queryCode string,
	queryMessage string,
	arguments ...any,
) (*EntryEntity, bool, error) {
	type entryResult struct {
		entry *EntryEntity
		found bool
	}

	result, err := readOnlyTransactionValue(ctx, repository.sql, func(provider ksql.Provider) (entryResult, error) {
		var row entryRow
		if queryErr := provider.QueryOne(ctx, &row, query, arguments...); queryErr != nil {
			if errors.Is(queryErr, ksql.ErrRecordNotFound) {
				return entryResult{entry: nil, found: false}, nil
			}

			return entryResult{}, oops.In("database").Code(queryCode).Wrapf(queryErr, "%s", queryMessage)
		}

		entry, scanErr := entryFromRow(&row)
		if scanErr != nil {
			return entryResult{}, oops.In("database").Code("scan_entry").Wrapf(scanErr, "scan entry")
		}

		snapshot := repository.withProvider(provider)
		if hydrateErr := snapshot.hydrateEntryMessage(ctx, entry); hydrateErr != nil {
			return entryResult{}, hydrateErr
		}

		return entryResult{entry: entry, found: true}, nil
	})
	if err != nil {
		return nil, false, err
	}

	return result.value.entry, result.value.found, nil
}

// DeleteEntryBranch removes an entry and all descendants from one session.
func (repository *SessionRepository) DeleteEntryBranch(ctx context.Context, sessionID, entryID string) error {
	now := repository.now().UTC()
	if err := repository.sql.Transaction(ctx, func(transaction ksql.Provider) error {
		return repository.deleteEntryBranchTx(ctx, transaction, sessionID, entryID, now)
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
	now time.Time,
) error {
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
	if _, err := transaction.Exec(ctx, touchSession, formatTime(now), sessionID); err != nil {
		return oops.In("database").Code("touch_after_delete_branch").Wrapf(err, "touch session after delete branch")
	}

	return nil
}

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

	return repository.queryEntries(ctx, query, "list_children", "scan_child", "children", args...)
}

func (repository *SessionRepository) queryEntries(
	ctx context.Context,
	query string,
	queryCode string,
	scanCode string,
	operation string,
	arguments ...any,
) ([]EntryEntity, error) {
	result, err := readOnlyTransactionValue(ctx, repository.sql, func(provider ksql.Provider) ([]EntryEntity, error) {
		rows := []entryRow{}
		if err := provider.Query(ctx, &rows, query, arguments...); err != nil {
			return nil, oops.In("database").Code(queryCode).Wrapf(err, "query session %s", operation)
		}

		entries, err := entriesFromRows(rows)
		if err != nil {
			return nil, oops.In("database").Code(scanCode).Wrapf(err, "scan session %s", operation)
		}

		if err := repository.withProvider(provider).hydrateEntryMessages(ctx, entries); err != nil {
			return nil, err
		}

		return entries, nil
	})
	if err != nil {
		return nil, err
	}

	return result.value, nil
}

func (repository *SessionRepository) appendCompactionConditional(
	ctx context.Context,
	entry *EntryEntity,
	operationID string,
) (*EntryEntity, error) {
	if strings.TrimSpace(operationID) == "" {
		return nil, oops.In("database").Code("compaction_operation_required").
			Errorf("compaction operation id is required")
	}

	if err := repository.prepareAppendEntry(entry); err != nil {
		return nil, err
	}

	if err := validateCompactionParent(ctx, repository.sql, entry); err != nil {
		return nil, err
	}

	resolved, err := transactionValue(ctx, repository.sql, func(transaction ksql.Provider) (*EntryEntity, error) {
		return repository.appendCompactionTx(ctx, transaction, entry, operationID)
	})
	if err != nil {
		if codedErr, ok := oops.AsOops(err); ok && codedErr.Code() != "" {
			return nil, oops.In("database").Wrapf(err, "append compaction transaction")
		}

		return nil, oops.In("database").Code("append_compaction_tx").Wrapf(err, "append compaction transaction")
	}

	if err := repository.hydrateEntryMessage(ctx, resolved.value); err != nil {
		return nil, err
	}

	return resolved.value, nil
}

func (repository *SessionRepository) appendCompactionTx(
	ctx context.Context,
	transaction ksql.Provider,
	entry *EntryEntity,
	operationID string,
) (*EntryEntity, error) {
	inserted, err := repository.insertEntryIgnoringConflictTx(ctx, transaction, entry, operationID, true)
	if err != nil {
		return nil, err
	}

	if !inserted {
		return resolveCompactionConflict(ctx, transaction, entry, operationID)
	}

	if err := repository.appendEntryMessage(ctx, transaction, entry); err != nil {
		return nil, err
	}

	const touchSession = `UPDATE sessions SET updated_at = ? WHERE id = ?`
	if _, err := transaction.Exec(ctx, touchSession, formatTime(entry.CreatedAt), entry.SessionID); err != nil {
		return nil, oops.In("database").Code("touch_session").Wrapf(err, "touch session")
	}

	return entry, nil
}

func resolveCompactionConflict(
	ctx context.Context,
	transaction ksql.Provider,
	entry *EntryEntity,
	operationID string,
) (*EntryEntity, error) {
	existing, found, err := entryByOperationID(ctx, transaction, operationID)
	if err != nil {
		return nil, err
	}

	if found {
		if !sameCompactionTarget(existing, entry) {
			return nil, oops.In("database").Code("compaction_operation_mismatch").
				Errorf("compaction operation id targets another branch")
		}

		return existing, nil
	}

	winner, found, err := compactionByParent(ctx, transaction, entry.SessionID, entry.ParentID)
	if err != nil {
		return nil, err
	}

	if found {
		return nil, oops.In("database").Code("stale_compaction_parent").With("winner_entry_id", winner.ID).
			Wrapf(ErrStaleCompactionParent, "append compaction")
	}

	return nil, oops.In("database").Code("compaction_insert_conflict").Errorf("compaction insert conflicted")
}

func entryByOperationID(
	ctx context.Context,
	provider ksql.Provider,
	operationID string,
) (*EntryEntity, bool, error) {
	query := fmt.Sprintf(`SELECT %s FROM session_entries WHERE operation_id = ?`, entrySelectColumns)

	return querySQLRow(ctx, provider, entryFromRow, query, "compaction_operation", operationID)
}

func compactionByParent(
	ctx context.Context,
	provider ksql.Provider,
	sessionID string,
	parentID *string,
) (*EntryEntity, bool, error) {
	query := fmt.Sprintf(`SELECT %s FROM session_entries
WHERE session_id = ? AND entry_type = ? AND parent_id IS NULL`, entrySelectColumns)
	args := []any{sessionID, EntryTypeCompaction}

	if parentID != nil {
		query = fmt.Sprintf(`SELECT %s FROM session_entries
WHERE session_id = ? AND entry_type = ? AND parent_id = ?`, entrySelectColumns)

		args = append(args, *parentID)
	}

	return querySQLRow(ctx, provider, entryFromRow, query, "compaction_parent", args...)
}

func validateCompactionParent(ctx context.Context, provider ksql.Provider, entry *EntryEntity) error {
	if entry.ParentID == nil {
		return nil
	}

	const query = `SELECT 1 AS present FROM session_entries WHERE session_id = ? AND id = ?`

	var row struct {
		Present int `ksql:"present"`
	}
	if err := provider.QueryOne(ctx, &row, query, entry.SessionID, *entry.ParentID); err != nil {
		if errors.Is(err, ksql.ErrRecordNotFound) {
			return oops.In("database").Code("compaction_parent_missing").
				Errorf("compaction parent is not in the session")
		}

		return oops.In("database").Code("compaction_parent").Wrapf(err, "load compaction parent")
	}

	return nil
}

func sameCompactionTarget(left, right *EntryEntity) bool {
	if left.SessionID != right.SessionID || left.Type != EntryTypeCompaction || right.Type != EntryTypeCompaction {
		return false
	}

	if left.ParentID == nil || right.ParentID == nil {
		return left.ParentID == nil && right.ParentID == nil
	}

	return *left.ParentID == *right.ParentID
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
	canonicalizeEntryMessageParts(entry)

	if err := applyEntryMetadata(entry); err != nil {
		return oops.In("database").Code("entry_metadata").Wrapf(err, "prepare entry metadata")
	}

	if err := validateEntryEntity(entry); err != nil {
		return oops.In("database").Code("validate_entry").Wrapf(err, "validate entry")
	}

	return nil
}

func canonicalizeEntryMessageParts(entry *EntryEntity) {
	if !entryCarriesMessage(entry) || len(entry.Message.Parts) != 0 || strings.TrimSpace(entry.Message.Content) == "" {
		return
	}

	entry.Message.Parts = []MessagePartEntity{{
		Type: MessagePartText, Text: entry.Message.Content, Data: nil,
		MIMEType: "", Name: "", Width: 0, Height: 0,
	}}
}

func (repository *SessionRepository) insertEntryTx(
	ctx context.Context,
	transaction ksql.Provider,
	entry *EntryEntity,
) error {
	inserted, err := repository.insertEntryIgnoringConflictTx(ctx, transaction, entry, "", false)
	if err != nil {
		return err
	}

	if !inserted {
		return oops.In("database").Code("append_entry").Errorf("append session entry was ignored")
	}

	return nil
}

func (repository *SessionRepository) insertEntryIgnoringConflictTx(
	ctx context.Context,
	transaction ksql.Provider,
	entry *EntryEntity,
	operationID string,
	ignoreConflict bool,
) (bool, error) {
	conflictClause := ""
	if ignoreConflict {
		conflictClause = " ON CONFLICT DO NOTHING"
	}

	insertEntry := `
INSERT INTO session_entries (
    id, session_id, parent_id, entry_type, custom_type, data_json, summary, created_at,
    tool_name, tool_status, tool_args_json, token_estimate, model_facing, display,
    compaction_first_kept_entry_id, compaction_tokens_before, branch_from_entry_id, operation_id
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)` + conflictClause

	result, err := transaction.Exec(
		ctx,
		insertEntry,
		entry.ID,
		entry.SessionID,
		entry.ParentID,
		string(entry.Type),
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
		operationID,
	)
	if err != nil {
		return false, oops.In("database").Code("append_entry").Wrapf(err, "append session entry")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return false, oops.In("database").Code("entry_rows_affected").Wrapf(err, "read affected entry rows")
	}

	return rows == 1, nil
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
