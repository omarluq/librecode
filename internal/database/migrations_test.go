package database_test

import (
	"context"
	"database/sql"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/testutil"
)

const (
	schemaIndexType = "index"

	deployedWorkflowMigrationV8 = `-- +goose Up
CREATE TABLE workflow_runs (
    task_id TEXT PRIMARY KEY,
    source TEXT NOT NULL,
    source_hash TEXT NOT NULL,
    source_version TEXT NOT NULL DEFAULT '',
    arguments_json TEXT NOT NULL DEFAULT '{}',
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
);
CREATE TABLE workflow_agent_tasks (
    workflow_task_id TEXT NOT NULL,
    agent_task_id TEXT NOT NULL UNIQUE,
    sequence INTEGER NOT NULL,
    node_key TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    PRIMARY KEY (workflow_task_id, agent_task_id),
    UNIQUE (workflow_task_id, sequence),
    FOREIGN KEY (workflow_task_id) REFERENCES workflow_runs(task_id) ON DELETE CASCADE,
    FOREIGN KEY (agent_task_id) REFERENCES agent_tasks(task_id) ON DELETE CASCADE
);
CREATE INDEX idx_workflow_agent_tasks_replay
    ON workflow_agent_tasks(workflow_task_id, sequence);
-- +goose Down
DROP TABLE IF EXISTS workflow_agent_tasks;
DROP TABLE IF EXISTS workflow_runs;
`
)

func TestTranscriptTailIndexMigrationUsesCursorColumns(t *testing.T) {
	t.Parallel()

	connection := newMigratedThroughVersion(t, 20)
	ctx := t.Context()
	migrationRoot, err := database.MigrationFS()
	require.NoError(t, err)
	provider, err := database.NewMigrationProvider(connection, migrationRoot)
	require.NoError(t, err)

	_, err = provider.UpTo(ctx, 21)
	require.NoError(t, err)
	assertIndexColumns(ctx, t, connection, "idx_session_messages_session_created", []string{
		"session_id", "created_at", "entry_id",
	})

	_, err = provider.Down(ctx)
	require.NoError(t, err)
	assertIndexColumns(ctx, t, connection, "idx_session_messages_session_created", []string{
		"session_id", "created_at",
	})
}

func assertIndexColumns(ctx context.Context, t *testing.T, connection *sql.DB, indexName string, want []string) {
	t.Helper()

	rows, err := connection.QueryContext(ctx, `SELECT name FROM pragma_index_info(?) ORDER BY seqno`, indexName)

	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()

	got := []string{}

	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		got = append(got, name)
	}

	require.NoError(t, rows.Err())
	assert.Equal(t, want, got)
}

func TestMessagePartsMigrationUpDownAndOldSchemaUpgrade(t *testing.T) {
	t.Parallel()

	connection := newMigratedThroughVersion(t, 12)
	ctx := context.Background()
	assertSchemaObjectMissing(ctx, t, connection,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'session_message_parts'`)

	migrationRoot, err := database.MigrationFS()
	require.NoError(t, err)
	provider, err := database.NewMigrationProvider(connection, migrationRoot)
	require.NoError(t, err)
	_, err = provider.UpTo(ctx, 14)
	require.NoError(t, err)

	assertSchemaObjectExists(ctx, t, connection,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'session_message_parts'`)

	for _, indexName := range []string{
		"idx_session_message_parts_session_entry_sequence",
		"idx_session_message_parts_session_entry_image",
	} {
		assertSchemaObjectExists(ctx, t, connection,
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, indexName)
	}

	repository := testutil.SessionRepository(t, connection)
	session, err := repository.CreateSession(ctx, "/work", "parts constraints", "")
	require.NoError(t, err)
	entry, err := repository.AppendMessage(ctx, session.ID, nil, &database.MessageEntity{
		Timestamp: time.Now().UTC(), Role: database.RoleUser, Content: testMessageText,
		Provider: "", Model: "", Parts: nil,
	})
	require.NoError(t, err)

	_, err = connection.ExecContext(ctx, `
INSERT INTO session_message_parts (id, session_id, entry_id, sequence, type, text)
VALUES (NULL, ?, ?, 1, 'text', 'invalid')`, session.ID, entry.ID)
	require.ErrorContains(t, err, "NOT NULL constraint failed")
	_, err = connection.ExecContext(ctx, `
INSERT INTO session_message_parts (id, session_id, entry_id, sequence, type, text)
VALUES ('fractional-sequence', ?, ?, 1.5, 'text', 'invalid')`, session.ID, entry.ID)
	require.ErrorContains(t, err, "CHECK constraint failed")

	_, err = provider.Down(ctx)
	require.NoError(t, err)
	assertSchemaObjectExists(ctx, t, connection,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'session_message_parts'`)

	const imagePartsIndexQuery = `SELECT COUNT(*) FROM sqlite_master
WHERE type = 'index' AND name = 'idx_session_message_parts_session_entry_image'`
	assertSchemaObjectMissing(ctx, t, connection, imagePartsIndexQuery)

	_, err = provider.Down(ctx)
	require.NoError(t, err)
	assertSchemaObjectMissing(ctx, t, connection,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'session_message_parts'`)
}

func TestWorkflowGuestAPIVersionMigrationPreservesLegacyRuns(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	connection := newMigratedThroughVersion(t, 15)
	_, err := connection.ExecContext(ctx, `
INSERT INTO sessions (id, cwd, name, created_at, updated_at)
VALUES ('01900000-0000-7000-8000-000000000201', '/tmp', 'owner', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
INSERT INTO tasks (id, kind, state, owner_session_id, created_at, updated_at)
VALUES ('01900000-0000-7000-8000-000000000202', 'workflow', 'queued',
        '01900000-0000-7000-8000-000000000201', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
INSERT INTO workflow_runs (task_id, source, source_hash, source_version, arguments_json, name)
VALUES ('01900000-0000-7000-8000-000000000202', '1', 'hash', 'v1', '{}', 'legacy')`)
	require.NoError(t, err)

	migrationRoot, err := database.MigrationFS()
	require.NoError(t, err)
	provider, err := database.NewMigrationProvider(connection, migrationRoot)
	require.NoError(t, err)
	_, err = provider.UpTo(ctx, 16)
	require.NoError(t, err)

	var version string
	require.NoError(t, connection.QueryRowContext(ctx, `
SELECT guest_api_version FROM workflow_runs
WHERE task_id = '01900000-0000-7000-8000-000000000202'`).Scan(&version))
	assert.Empty(t, version)

	_, err = connection.ExecContext(ctx, `
INSERT INTO tasks (id, kind, state, owner_session_id, created_at, updated_at)
VALUES ('01900000-0000-7000-8000-000000000203', 'workflow', 'queued',
        '01900000-0000-7000-8000-000000000201', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
INSERT INTO workflow_runs (
    task_id, source, source_hash, source_version, guest_api_version, arguments_json, name
) VALUES ('01900000-0000-7000-8000-000000000203', '2', 'hash', 'v1', '2', '{}', 'v2')`)
	require.NoError(t, err)

	_, err = provider.Down(ctx)
	require.NoError(t, err)
	assertSchemaObjectMissing(ctx, t, connection,
		`SELECT COUNT(*) FROM pragma_table_info('workflow_runs') WHERE name = 'guest_api_version'`)
}

func TestPersistedCompatibilityMigrationRejectsRollback(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	connection := newMigratedThroughVersion(t, 19)
	migrationRoot, err := database.MigrationFS()
	require.NoError(t, err)
	provider, err := database.NewMigrationProvider(connection, migrationRoot)
	require.NoError(t, err)

	_, err = provider.UpTo(ctx, 20)
	require.NoError(t, err)
	_, err = provider.Down(ctx)
	require.ErrorContains(t, err, "migration_20_is_irreversible")

	version, err := provider.GetDBVersion(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(20), version)
	assertSchemaObjectMissing(ctx, t, connection,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'migration_20_is_irreversible'`)
}

func TestToolTasksMigrationUpDownAndVersionFourteenUpgrade(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	connection := newMigratedThroughVersion(t, 14)
	_, err := connection.ExecContext(ctx, `PRAGMA foreign_keys=ON`)
	require.NoError(t, err)

	_, err = connection.ExecContext(ctx, `
INSERT INTO sessions (id, cwd, name, created_at, updated_at)
VALUES ('01900000-0000-7000-8000-000000000101', '/tmp', 'owner', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
       ('01900000-0000-7000-8000-000000000102', '/tmp', 'other', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
INSERT INTO tasks (id, kind, state, owner_session_id, created_at, updated_at)
VALUES ('01900000-0000-7000-8000-000000000103', 'tool', 'queued',
        '01900000-0000-7000-8000-000000000101', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
       ('01900000-0000-7000-8000-000000000104', 'tool', 'queued',
        '01900000-0000-7000-8000-000000000101', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)
	require.NoError(t, err)

	migrationRoot, err := database.MigrationFS()
	require.NoError(t, err)
	provider, err := database.NewMigrationProvider(connection, migrationRoot)
	require.NoError(t, err)
	_, err = provider.UpTo(ctx, 15)
	require.NoError(t, err)

	for _, indexName := range []string{
		"idx_tasks_id_owner_tool",
		"idx_tool_tasks_owner_invocation",
		"idx_tool_tasks_owner_target",
	} {
		assertSchemaObjectExists(ctx, t, connection,
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, indexName)
	}

	assertSchemaObjectExists(ctx, t, connection,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'tool_tasks'`)
	assertSchemaObjectExists(ctx, t, connection,
		`SELECT COUNT(*) FROM pragma_table_info('tool_tasks') WHERE name = 'task_id' AND "notnull" = 1`)

	const insertToolTask = `INSERT INTO tool_tasks (
    task_id, target_name, arguments_json, cwd, owner_session_id, invocation_id,
    wrapper_call_id, timeout_seconds, policy_json, definition_json
) VALUES (?, 'read', '{}', '/tmp', ?, ?, 'wrapper-call', 30, '{}', '{}')`

	_, err = connection.ExecContext(ctx, insertToolTask,
		"01900000-0000-7000-8000-000000000103",
		"01900000-0000-7000-8000-000000000101",
		"invocation-1",
	)
	require.NoError(t, err)

	_, err = connection.ExecContext(ctx, insertToolTask,
		"01900000-0000-7000-8000-000000000104",
		"01900000-0000-7000-8000-000000000102",
		"invocation-2",
	)
	require.ErrorContains(t, err, "FOREIGN KEY constraint failed")

	_, err = provider.Down(ctx)
	require.NoError(t, err)
	assertSchemaObjectMissing(ctx, t, connection,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'tool_tasks'`)

	for _, indexName := range []string{
		"idx_tasks_id_owner_tool",
		"idx_tool_tasks_owner_invocation",
		"idx_tool_tasks_owner_target",
	} {
		assertSchemaObjectMissing(ctx, t, connection,
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, indexName)
	}

	assertSchemaObjectExists(ctx, t, connection,
		`SELECT COUNT(*) FROM tasks WHERE id = '01900000-0000-7000-8000-000000000103'`)
}

func TestMessagePartsMigrationRejectsPreexistingSchemaObjects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		setup      string
		objectType string
		objectName string
	}{
		{
			name:       "index",
			setup:      `CREATE INDEX idx_session_entries_id_session ON session_entries(session_id)`,
			objectType: schemaIndexType,
			objectName: "idx_session_entries_id_session",
		},
		{
			name:       "table",
			setup:      `CREATE TABLE session_message_parts (sentinel TEXT NOT NULL)`,
			objectType: "table",
			objectName: "session_message_parts",
		},
		{
			name: "image index",
			setup: `
CREATE UNIQUE INDEX idx_session_entries_id_session ON session_entries(id, session_id);
CREATE TABLE session_message_parts (
    id TEXT NOT NULL PRIMARY KEY,
    session_id TEXT NOT NULL,
    entry_id TEXT NOT NULL,
    sequence INTEGER NOT NULL,
    type TEXT NOT NULL
);
CREATE INDEX idx_session_message_parts_session_entry_sequence
    ON session_message_parts(session_id, entry_id, sequence);
INSERT INTO goose_db_version (version_id, is_applied) VALUES (13, 1);
CREATE INDEX idx_session_message_parts_session_entry_image
    ON session_message_parts(entry_id)`,
			objectType: schemaIndexType,
			objectName: "idx_session_message_parts_session_entry_image",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			connection := newMigratedThroughVersion(t, 12)
			ctx := context.Background()
			_, err := connection.ExecContext(ctx, test.setup)
			require.NoError(t, err)

			migrationRoot, migrationErr := database.MigrationFS()
			require.NoError(t, migrationErr)
			provider, migrationErr := database.NewMigrationProvider(connection, migrationRoot)
			require.NoError(t, migrationErr)
			_, migrationErr = provider.Up(ctx)
			require.Error(t, migrationErr)

			assertSchemaObjectExists(ctx, t, connection,
				`SELECT COUNT(*) FROM sqlite_master WHERE type = ? AND name = ?`,
				test.objectType, test.objectName)
		})
	}
}

func assertSchemaObjectMissing(
	ctx context.Context,
	t *testing.T,
	connection *sql.DB,
	query string,
	args ...any,
) {
	t.Helper()

	var count int
	require.NoError(t, connection.QueryRowContext(ctx, query, args...).Scan(&count))
	assert.Zero(t, count)
}

func TestMigrateAddsCompactionOperationIdentityAfterPreviouslyDeployedVersionEleven(t *testing.T) {
	t.Parallel()

	connection := newMigratedThroughVersion(t, 10)
	ctx := context.Background()
	_, err := connection.ExecContext(ctx, `
INSERT INTO goose_db_version (version_id, is_applied)
VALUES (11, 1)`)
	require.NoError(t, err)

	require.NoError(t, database.Migrate(ctx, connection))

	assertSchemaObjectExists(ctx, t, connection,
		`SELECT COUNT(*) FROM pragma_table_info('session_entries') WHERE name = 'operation_id'`)

	for _, indexName := range []string{
		"idx_session_entries_operation_id",
		"idx_session_entries_compaction_parent",
	} {
		assertSchemaObjectExists(ctx, t, connection,
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, indexName)
	}
}

func assertSchemaObjectExists(
	ctx context.Context,
	t *testing.T,
	connection *sql.DB,
	query string,
	args ...any,
) {
	t.Helper()

	var count int
	require.NoError(t, connection.QueryRowContext(ctx, query, args...).Scan(&count))
	assert.Equal(t, 1, count)
}

func TestMigrateAddsWorkflowInvocationIndexToDeployedVersionEightDatabase(t *testing.T) {
	t.Parallel()

	connection := newMigratedThroughVersion(t, 7)
	ctx := context.Background()
	migrationRoot := fstest.MapFS{
		"00008_create_workflow_runs.sql": &fstest.MapFile{Data: []byte(deployedWorkflowMigrationV8)},
	}
	provider, err := database.NewMigrationProvider(connection, migrationRoot)
	require.NoError(t, err)

	_, err = provider.Up(ctx)
	require.NoError(t, err)

	insertWorkflowMigrationFixtures(ctx, t, connection)
	require.NoError(t, database.Migrate(ctx, connection))

	assertWorkflowInvocationColumn(ctx, t, connection)
	assertWorkflowInvocationIndexes(ctx, t, connection)
}

func insertWorkflowMigrationFixtures(ctx context.Context, t *testing.T, connection *sql.DB) {
	t.Helper()

	_, err := connection.ExecContext(ctx, `
INSERT INTO sessions (id, cwd, name, created_at, updated_at)
VALUES ('01900000-0000-7000-8000-000000000001', '/tmp', 'owner', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
       ('01900000-0000-7000-8000-000000000004', '/tmp', 'child', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
       ('01900000-0000-7000-8000-000000000006', '/tmp', 'second child', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)
	require.NoError(t, err)

	insertWorkflowTasks(ctx, t, connection)
	insertWorkflowAgentLinks(ctx, t, connection)
}

func insertWorkflowTasks(ctx context.Context, t *testing.T, connection *sql.DB) {
	t.Helper()

	const ownerID = "01900000-0000-7000-8000-000000000001"

	_, err := connection.ExecContext(ctx, `
INSERT INTO tasks (id, kind, state, owner_session_id, created_at, updated_at)
VALUES ('01900000-0000-7000-8000-000000000002', 'workflow', 'queued', ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
       ('01900000-0000-7000-8000-000000000003', 'agent', 'queued', ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
       ('01900000-0000-7000-8000-000000000005', 'agent', 'queued', ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		ownerID, ownerID, ownerID)
	require.NoError(t, err)
}

func insertWorkflowAgentLinks(ctx context.Context, t *testing.T, connection *sql.DB) {
	t.Helper()

	_, err := connection.ExecContext(ctx, `
INSERT INTO workflow_runs (task_id, source, source_hash)
VALUES ('01900000-0000-7000-8000-000000000002', 'package main', 'hash')`)
	require.NoError(t, err)

	_, err = connection.ExecContext(ctx, `
INSERT INTO agent_tasks (task_id, child_session_id, agent_name, prompt, depth)
VALUES ('01900000-0000-7000-8000-000000000003', '01900000-0000-7000-8000-000000000004', 'reviewer', 'review', 1),
       ('01900000-0000-7000-8000-000000000005', '01900000-0000-7000-8000-000000000006', 'reviewer', 'review again', 1)`)
	require.NoError(t, err)

	_, err = connection.ExecContext(ctx, `
INSERT INTO workflow_agent_tasks
    (workflow_task_id, agent_task_id, sequence, node_key, created_at)
VALUES
    ('01900000-0000-7000-8000-000000000002', '01900000-0000-7000-8000-000000000003', 1, 'review', CURRENT_TIMESTAMP),
    ('01900000-0000-7000-8000-000000000002', '01900000-0000-7000-8000-000000000005', 2, 'review', CURRENT_TIMESTAMP)`)
	require.NoError(t, err)
}

func assertWorkflowInvocationColumn(ctx context.Context, t *testing.T, connection *sql.DB) {
	t.Helper()

	columns, err := connection.QueryContext(ctx, `PRAGMA table_info(workflow_agent_tasks)`)
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, columns.Close()) })

	found := false

	for columns.Next() {
		var (
			cid, notNull, primaryKey int
			name, columnType         string
			defaultValue             sql.NullString
		)

		require.NoError(t, columns.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey))

		if name != "invocation_index" {
			continue
		}

		found = true

		assert.Equal(t, 1, notNull)
		assert.Equal(t, "0", defaultValue.String)
	}

	require.NoError(t, columns.Err())
	require.True(t, found)
}

func assertWorkflowInvocationIndexes(ctx context.Context, t *testing.T, connection *sql.DB) {
	t.Helper()

	rows, err := connection.QueryContext(ctx,
		`SELECT invocation_index FROM workflow_agent_tasks ORDER BY sequence`)
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, rows.Close()) })

	var invocationIndexes []int

	for rows.Next() {
		var invocationIndex int

		require.NoError(t, rows.Scan(&invocationIndex))
		invocationIndexes = append(invocationIndexes, invocationIndex)
	}

	require.NoError(t, rows.Err())
	assert.Equal(t, []int{0, 1}, invocationIndexes)
}
