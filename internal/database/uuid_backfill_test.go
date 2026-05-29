package database_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

const legacyEntryDataJSON = `{"fromId":"entry-root","targetId":"entry-root",` +
	`"firstKeptEntryId":"entry-root","compactionFirstKeptEntryId":"entry-root",` +
	`"branchFromEntryId":"entry-root"}`

func TestBackfillUUIDv7IDsRewritesSessionGraph(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	connection := newMigratedThroughVersion(t, 4)
	insertLegacySessionGraph(ctx, t, connection)

	require.NoError(t, database.Migrate(ctx, connection))

	oldIDs := []string{"session-old", "entry-root", "entry-child", "message-root", "message-child"}
	for _, oldID := range oldIDs {
		assertNoRowWithID(ctx, t, connection, oldID)
	}

	var sessionID string
	require.NoError(t, connection.QueryRowContext(ctx, `SELECT id FROM sessions`).Scan(&sessionID))
	assertUUIDV7(t, sessionID)

	entries := queryEntryBackfillRows(ctx, t, connection)
	require.Len(t, entries, 2)
	rootID := entries[0].id
	childID := entries[1].id
	assertUUIDV7(t, rootID)
	assertUUIDV7(t, childID)
	require.True(t, entries[1].parentID.Valid)
	assert.Equal(t, rootID, entries[1].parentID.String)
	assert.Equal(t, rootID, entries[1].compactionFirstKeptEntryID)
	assert.Equal(t, rootID, entries[1].branchFromEntryID)

	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(entries[1].dataJSON), &data))
	assert.Equal(t, rootID, data["fromId"])
	assert.Equal(t, rootID, data["targetId"])
	assert.Equal(t, rootID, data["firstKeptEntryId"])
	assert.Equal(t, rootID, data["compactionFirstKeptEntryId"])
	assert.Equal(t, rootID, data["branchFromEntryId"])

	messages := queryMessageBackfillRows(ctx, t, connection)
	require.Len(t, messages, 2)
	assertUUIDV7(t, messages[0].id)
	assertUUIDV7(t, messages[1].id)
	assert.Equal(t, sessionID, messages[0].sessionID)
	assert.Equal(t, rootID, messages[0].entryID)
	assert.Equal(t, childID, messages[1].entryID)
}

func TestBackfillUUIDv7IDsIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	connection := newMigratedThroughVersion(t, 5)
	repository := database.NewSessionRepository(connection)
	session, err := repository.CreateSession(ctx, "/work", "", "")
	require.NoError(t, err)
	entry := appendTestMessage(ctx, t, repository, session.ID, nil, database.RoleUser, "hello")

	require.NoError(t, database.BackfillUUIDv7IDs(ctx, connection))

	foundEntry, found, err := repository.Entry(ctx, session.ID, entry.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, entry.ID, foundEntry.ID)
}

func insertLegacySessionGraph(ctx context.Context, t *testing.T, connection *sql.DB) {
	t.Helper()

	createdAt := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := connection.ExecContext(
		ctx,
		`INSERT INTO sessions (id, cwd, name, parent_session, created_at, updated_at)
VALUES ('session-old', '/work', '', '', ?, ?)`,
		createdAt,
		createdAt,
	)
	require.NoError(t, err)
	insertLegacyEntries(ctx, t, connection, createdAt)
	insertLegacyMessages(ctx, t, connection, createdAt)
}

func insertLegacyEntries(ctx context.Context, t *testing.T, connection *sql.DB, createdAt string) {
	t.Helper()

	_, err := connection.ExecContext(
		ctx,
		`INSERT INTO session_entries (
    id, session_id, parent_id, entry_type, role, content,
    provider, model, custom_type, data_json, summary, created_at,
    tool_name, tool_status, tool_args_json, token_estimate, model_facing, display,
    compaction_first_kept_entry_id, compaction_tokens_before, branch_from_entry_id
) VALUES (
    'entry-root', 'session-old', NULL, 'message', 'user', 'hello',
    '', '', '', '{}', '', ?,
    '', '', '', 1, 1, 1, '', 0, ''
), (
    'entry-child', 'session-old', 'entry-root', 'message', 'assistant', 'hi',
    '', '', '', ?, '', ?,
    '', '', '', 1, 1, 1, 'entry-root', 10, 'entry-root'
)`,
		createdAt,
		legacyEntryDataJSON,
		createdAt,
	)
	require.NoError(t, err)
}

func insertLegacyMessages(ctx context.Context, t *testing.T, connection *sql.DB, createdAt string) {
	t.Helper()

	_, err := connection.ExecContext(
		ctx,
		`INSERT INTO session_messages (id, session_id, entry_id, sender, role, content, provider, model, created_at)
VALUES
    ('message-root', 'session-old', 'entry-root', 'user', 'user', 'hello', '', '', ?),
    ('message-child', 'session-old', 'entry-child', 'assistant', 'assistant', 'hi', '', '', ?)`,
		createdAt,
		createdAt,
	)
	require.NoError(t, err)
}

type entryBackfillRow struct {
	id                         string
	dataJSON                   string
	compactionFirstKeptEntryID string
	branchFromEntryID          string
	parentID                   sql.NullString
}

func queryEntryBackfillRows(ctx context.Context, t *testing.T, connection *sql.DB) []entryBackfillRow {
	t.Helper()

	rows, err := connection.QueryContext(
		ctx,
		`SELECT id, parent_id, data_json, compaction_first_kept_entry_id, branch_from_entry_id
FROM session_entries ORDER BY created_at ASC, role DESC`,
	)
	require.NoError(t, err)

	entries := []entryBackfillRow{}
	for rows.Next() {
		entry := entryBackfillRow{
			parentID:                   sql.NullString{},
			id:                         "",
			dataJSON:                   "",
			compactionFirstKeptEntryID: "",
			branchFromEntryID:          "",
		}
		require.NoError(t, rows.Scan(
			&entry.id,
			&entry.parentID,
			&entry.dataJSON,
			&entry.compactionFirstKeptEntryID,
			&entry.branchFromEntryID,
		))
		entries = append(entries, entry)
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())

	return entries
}

type messageBackfillRow struct {
	id        string
	sessionID string
	entryID   string
}

func queryMessageBackfillRows(ctx context.Context, t *testing.T, connection *sql.DB) []messageBackfillRow {
	t.Helper()

	rows, err := connection.QueryContext(
		ctx,
		`SELECT id, session_id, entry_id FROM session_messages ORDER BY created_at ASC, id ASC`,
	)
	require.NoError(t, err)

	messages := []messageBackfillRow{}
	for rows.Next() {
		message := messageBackfillRow{
			id:        "",
			sessionID: "",
			entryID:   "",
		}
		require.NoError(t, rows.Scan(&message.id, &message.sessionID, &message.entryID))
		messages = append(messages, message)
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())

	return messages
}

func assertNoRowWithID(ctx context.Context, t *testing.T, connection *sql.DB, oldID string) {
	t.Helper()

	queries := []string{
		`SELECT count(*) FROM sessions WHERE id = ? OR parent_session = ?`,
		`SELECT count(*) FROM session_entries
WHERE id = ? OR session_id = ? OR parent_id = ? OR compaction_first_kept_entry_id = ? OR branch_from_entry_id = ?`,
		`SELECT count(*) FROM session_messages WHERE id = ? OR session_id = ? OR entry_id = ?`,
	}
	for _, query := range queries {
		args := repeatedArgs(oldID, strings.Count(query, "?"))
		var count int
		require.NoError(t, connection.QueryRowContext(ctx, query, args...).Scan(&count))
		assert.Zero(t, count)
	}
}

func repeatedArgs(value string, count int) []any {
	args := make([]any, count)
	for index := range args {
		args[index] = value
	}

	return args
}
