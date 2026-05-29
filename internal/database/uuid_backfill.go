package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

const closeUUIDv7ForeignKeyCheckRowsError = "database: close UUIDv7 foreign key check rows: %w"

type sessionIDRow struct {
	ID            string
	ParentSession string
}

type entryIDRow struct {
	ID                         string
	SessionID                  string
	CompactionFirstKeptEntryID string
	BranchFromEntryID          string
	DataJSON                   string
	ParentID                   sql.NullString
}

type messageIDRow struct {
	ID        string
	SessionID string
	EntryID   string
}

type uuidV7BackfillRows struct {
	sessions []sessionIDRow
	entries  []entryIDRow
	messages []messageIDRow
}

type uuidV7BackfillReplacements struct {
	sessions map[string]string
	entries  map[string]string
	messages map[string]string
}

// BackfillUUIDv7IDs rewrites existing session graph identifiers to UUIDv7 while preserving relationships.
func BackfillUUIDv7IDs(ctx context.Context, connection *sql.DB) (err error) {
	rows, err := loadIDRows(ctx, connection)
	if err != nil {
		return err
	}

	replacements := uuidV7ReplacementMaps(rows)
	if replacements.none() {
		return nil
	}

	foreignKeysEnabled, err := disableForeignKeysForUUIDv7Backfill(ctx, connection)
	if err != nil {
		return err
	}
	restoreForeignKeys := true
	defer func() {
		if restoreForeignKeys {
			err = errors.Join(err, restoreForeignKeySetting(context.WithoutCancel(ctx), connection, foreignKeysEnabled))
		}
	}()

	if err := runUUIDv7BackfillTransaction(ctx, connection, rows, replacements); err != nil {
		return err
	}
	if err := restoreForeignKeySetting(ctx, connection, foreignKeysEnabled); err != nil {
		return err
	}
	restoreForeignKeys = false

	return checkForeignKeys(ctx, connection)
}

func uuidV7ReplacementMaps(rows uuidV7BackfillRows) uuidV7BackfillReplacements {
	return uuidV7BackfillReplacements{
		sessions: uuidV7ReplacementMap(sessionIDsFromRows(rows.sessions)),
		entries:  uuidV7ReplacementMap(entryIDsFromRows(rows.entries)),
		messages: uuidV7ReplacementMap(messageIDsFromRows(rows.messages)),
	}
}

func (replacements uuidV7BackfillReplacements) none() bool {
	return len(replacements.sessions) == 0 && len(replacements.entries) == 0 && len(replacements.messages) == 0
}

func runUUIDv7BackfillTransaction(
	ctx context.Context,
	connection *sql.DB,
	rows uuidV7BackfillRows,
	replacements uuidV7BackfillReplacements,
) error {
	transaction, err := connection.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("database: begin UUIDv7 backfill: %w", err)
	}
	if err := backfillUUIDv7IDsTx(ctx, transaction, rows, replacements); err != nil {
		rollbackErr := transaction.Rollback()
		if rollbackErr != nil {
			return fmt.Errorf("database: rollback UUIDv7 backfill: %w", rollbackErr)
		}
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("database: commit UUIDv7 backfill: %w", err)
	}

	return nil
}

func loadIDRows(ctx context.Context, connection *sql.DB) (uuidV7BackfillRows, error) {
	sessions, err := queryRows(ctx, connection, `SELECT id, parent_session FROM sessions`, scanSessionIDRow)
	if err != nil {
		return uuidV7BackfillRows{}, err
	}
	entries, err := queryRows(
		ctx,
		connection,
		`SELECT id, session_id, parent_id, data_json,
compaction_first_kept_entry_id, branch_from_entry_id
FROM session_entries`,
		scanEntryIDRow,
	)
	if err != nil {
		return uuidV7BackfillRows{}, err
	}
	messages, err := queryRows(
		ctx,
		connection,
		`SELECT id, session_id, entry_id FROM session_messages`,
		scanMessageIDRow,
	)
	if err != nil {
		return uuidV7BackfillRows{}, err
	}

	return uuidV7BackfillRows{
		sessions: sessions,
		entries:  entries,
		messages: messages,
	}, nil
}

func scanSessionIDRow(scanner rowScanner) (*sessionIDRow, error) {
	row := sessionIDRow{
		ID:            "",
		ParentSession: "",
	}
	return &row, scanner.Scan(&row.ID, &row.ParentSession)
}

func scanEntryIDRow(scanner rowScanner) (*entryIDRow, error) {
	row := entryIDRow{
		ParentID:                   sql.NullString{},
		ID:                         "",
		SessionID:                  "",
		CompactionFirstKeptEntryID: "",
		BranchFromEntryID:          "",
		DataJSON:                   "",
	}
	return &row, scanner.Scan(
		&row.ID,
		&row.SessionID,
		&row.ParentID,
		&row.DataJSON,
		&row.CompactionFirstKeptEntryID,
		&row.BranchFromEntryID,
	)
}

func scanMessageIDRow(scanner rowScanner) (*messageIDRow, error) {
	row := messageIDRow{
		ID:        "",
		SessionID: "",
		EntryID:   "",
	}
	return &row, scanner.Scan(&row.ID, &row.SessionID, &row.EntryID)
}

func queryRows[T any](
	ctx context.Context,
	connection *sql.DB,
	query string,
	scan func(rowScanner) (*T, error),
) ([]T, error) {
	rows, err := connection.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("database: query UUIDv7 backfill rows: %w", err)
	}

	return collectRows(rows, scan, &rowErrorInfo{
		scanCode:  "scan_uuid_backfill",
		scanMsg:   "scan UUIDv7 backfill row",
		iterCode:  "iterate_uuid_backfill",
		iterMsg:   "iterate UUIDv7 backfill rows",
		closeCode: "close_uuid_backfill",
		closeMsg:  "close UUIDv7 backfill rows",
	})
}

func uuidV7ReplacementMap(ids []string) map[string]string {
	replacements := make(map[string]string)
	for _, id := range ids {
		if id == "" || isUUIDv7(id) {
			continue
		}
		replacements[id] = newUUIDv7()
	}

	return replacements
}

func sessionIDsFromRows(rows []sessionIDRow) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}

	return ids
}

func entryIDsFromRows(rows []entryIDRow) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}

	return ids
}

func messageIDsFromRows(rows []messageIDRow) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}

	return ids
}

func backfillUUIDv7IDsTx(
	ctx context.Context,
	transaction *sql.Tx,
	rows uuidV7BackfillRows,
	replacements uuidV7BackfillReplacements,
) error {
	if err := backfillSessionUUIDv7IDsTx(ctx, transaction, rows.sessions, replacements.sessions); err != nil {
		return err
	}
	if err := backfillEntryUUIDv7IDsTx(ctx, transaction, rows.entries, replacements); err != nil {
		return err
	}

	return backfillMessageUUIDv7IDsTx(ctx, transaction, rows.messages, replacements)
}

func backfillSessionUUIDv7IDsTx(
	ctx context.Context,
	transaction *sql.Tx,
	rows []sessionIDRow,
	sessionIDs map[string]string,
) error {
	for _, row := range rows {
		if _, err := transaction.ExecContext(
			ctx,
			`UPDATE sessions SET id = ?, parent_session = ? WHERE id = ?`,
			replaceID(sessionIDs, row.ID),
			replaceID(sessionIDs, row.ParentSession),
			row.ID,
		); err != nil {
			return fmt.Errorf("database: backfill session UUIDv7 IDs: %w", err)
		}
	}

	return nil
}

func backfillEntryUUIDv7IDsTx(
	ctx context.Context,
	transaction *sql.Tx,
	rows []entryIDRow,
	replacements uuidV7BackfillReplacements,
) error {
	for index := range rows {
		if err := backfillEntryUUIDv7IDTx(ctx, transaction, &rows[index], replacements); err != nil {
			return err
		}
	}

	return nil
}

func backfillEntryUUIDv7IDTx(
	ctx context.Context,
	transaction *sql.Tx,
	row *entryIDRow,
	replacements uuidV7BackfillReplacements,
) error {
	parentID := sql.NullString{}
	if row.ParentID.Valid {
		parentID = sql.NullString{String: replaceID(replacements.entries, row.ParentID.String), Valid: true}
	}
	dataJSON, err := replaceEntryDataIDs(row.DataJSON, replacements.entries)
	if err != nil {
		return err
	}
	_, err = transaction.ExecContext(
		ctx,
		`UPDATE session_entries
SET id = ?, session_id = ?, parent_id = ?, data_json = ?, compaction_first_kept_entry_id = ?, branch_from_entry_id = ?
WHERE id = ?`,
		replaceID(replacements.entries, row.ID),
		replaceID(replacements.sessions, row.SessionID),
		parentID,
		dataJSON,
		replaceID(replacements.entries, row.CompactionFirstKeptEntryID),
		replaceID(replacements.entries, row.BranchFromEntryID),
		row.ID,
	)
	if err != nil {
		return fmt.Errorf("database: backfill entry UUIDv7 IDs: %w", err)
	}

	return nil
}

func backfillMessageUUIDv7IDsTx(
	ctx context.Context,
	transaction *sql.Tx,
	rows []messageIDRow,
	replacements uuidV7BackfillReplacements,
) error {
	for _, row := range rows {
		if _, err := transaction.ExecContext(
			ctx,
			`UPDATE session_messages SET id = ?, session_id = ?, entry_id = ? WHERE id = ?`,
			replaceID(replacements.messages, row.ID),
			replaceID(replacements.sessions, row.SessionID),
			replaceID(replacements.entries, row.EntryID),
			row.ID,
		); err != nil {
			return fmt.Errorf("database: backfill message UUIDv7 IDs: %w", err)
		}
	}

	return nil
}

func replaceEntryDataIDs(dataJSON string, entryIDs map[string]string) (string, error) {
	if dataJSON == "" || dataJSON == "{}" || len(entryIDs) == 0 {
		return normalizeDataJSON(dataJSON), nil
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(dataJSON), &data); err != nil {
		return "", fmt.Errorf("database: decode UUIDv7 backfill entry data: %w", err)
	}
	for _, key := range []string{
		"fromId",
		"targetId",
		"firstKeptEntryId",
		"compactionFirstKeptEntryId",
		"branchFromEntryId",
	} {
		value, ok := data[key].(string)
		if !ok {
			continue
		}
		data[key] = replaceID(entryIDs, value)
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("database: encode UUIDv7 backfill entry data: %w", err)
	}

	return string(encoded), nil
}

func replaceID(replacements map[string]string, id string) string {
	if replacement, ok := replacements[id]; ok {
		return replacement
	}

	return id
}

func disableForeignKeysForUUIDv7Backfill(ctx context.Context, connection *sql.DB) (bool, error) {
	foreignKeysEnabled, err := currentForeignKeySetting(ctx, connection)
	if err != nil {
		return false, err
	}
	_, execErr := connection.ExecContext(ctx, `PRAGMA foreign_keys = OFF`)
	if execErr != nil {
		return false, fmt.Errorf("database: disable foreign keys for UUIDv7 backfill: %w", execErr)
	}

	return foreignKeysEnabled, nil
}

func currentForeignKeySetting(ctx context.Context, connection *sql.DB) (bool, error) {
	var enabled int
	if err := connection.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&enabled); err != nil {
		return false, fmt.Errorf("database: read foreign key setting: %w", err)
	}

	return enabled != 0, nil
}

func restoreForeignKeySetting(ctx context.Context, connection *sql.DB, enabled bool) error {
	if enabled {
		if _, err := connection.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
			return fmt.Errorf("database: restore foreign keys: %w", err)
		}
		return nil
	}
	if _, err := connection.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("database: restore foreign keys: %w", err)
	}

	return nil
}

func checkForeignKeys(ctx context.Context, connection *sql.DB) error {
	rows, err := connection.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("database: check foreign keys after UUIDv7 backfill: %w", err)
	}
	if rows.Next() {
		if closeErr := rows.Close(); closeErr != nil {
			return fmt.Errorf(closeUUIDv7ForeignKeyCheckRowsError, closeErr)
		}
		return fmt.Errorf("database: UUIDv7 backfill left invalid foreign keys")
	}
	if err := rows.Err(); err != nil {
		if closeErr := rows.Close(); closeErr != nil {
			return fmt.Errorf(closeUUIDv7ForeignKeyCheckRowsError, closeErr)
		}
		return fmt.Errorf("database: iterate UUIDv7 foreign key check: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf(closeUUIDv7ForeignKeyCheckRowsError, err)
	}

	return nil
}
