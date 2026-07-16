package database_test

import (
	"context"
	"database/sql"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

const deployedWorkflowMigrationV8 = `-- +goose Up
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
