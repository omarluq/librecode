package database_test

import (
	"context"
	"testing"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

func TestRepositoryRejectsInvalidUUIDs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	connection := newMigratedThroughVersion(t, 5)
	repository := database.NewSessionRepository(connection)
	session, err := repository.CreateSession(ctx, "/work", "uuid", "")
	require.NoError(t, err)

	validEntry := appendTestMessage(ctx, t, repository, session.ID, nil, database.RoleUser, "hello")
	now := time.Now().UTC().Format(time.RFC3339Nano)

	tests := []struct {
		query     string
		name      string
		wantError string
		args      []any
	}{
		{
			name: "session id trigger",
			query: `INSERT INTO sessions (id, cwd, name, parent_session, created_at, updated_at)
VALUES ('legacy-session', '/work', '', '', ?, ?)`,
			args:      []any{now, now},
			wantError: "sessions.id must be a UUIDv7",
		},
		{
			name: "entry id trigger",
			query: `INSERT INTO session_entries (
    id, session_id, parent_id, entry_type, role, content,
    provider, model, custom_type, data_json, summary, created_at,
    tool_name, tool_status, tool_args_json, token_estimate, model_facing, display,
    compaction_first_kept_entry_id, compaction_tokens_before, branch_from_entry_id
) VALUES (
    'legacy-entry', ?, NULL, 'message', 'user', 'hello',
    '', '', '', '{}', '', ?, '', '', '', 1, 1, 1, '', 0, ''
)`,
			args:      []any{session.ID, now},
			wantError: "session_entries.id must be a UUIDv7",
		},
		{
			name: "message entry id trigger",
			query: `INSERT INTO session_messages (
    id, session_id, entry_id, sender, role, content, provider, model, created_at
) VALUES (?, ?, 'legacy-entry', 'user', 'user', 'hello', '', '', ?)`,
			args:      []any{testUUIDV7(t), session.ID, now},
			wantError: "session_messages.entry_id must be a UUIDv7",
		},
		{
			name: "message id trigger",
			query: `INSERT INTO session_messages (
    id, session_id, entry_id, sender, role, content, provider, model, created_at
) VALUES ('legacy-message', ?, ?, 'user', 'user', 'hello', '', '', ?)`,
			args:      []any{session.ID, validEntry.ID, now},
			wantError: "session_messages.id must be a UUIDv7",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := connection.ExecContext(ctx, test.query, test.args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.wantError)
		})
	}
}

func testUUIDV7(t *testing.T) string {
	t.Helper()

	id, err := uuid.NewV7()
	require.NoError(t, err)

	return id.String()
}
