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

type invalidUUIDCase struct {
	args      func(*testing.T, *database.SessionEntity, *database.EntryEntity) []any
	query     string
	name      string
	wantError string
}

func TestRepositoryRejectsInvalidUUIDs(t *testing.T) {
	t.Parallel()

	for _, test := range invalidUUIDCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			connection := newMigratedThroughVersion(t, 5)
			repository := database.NewSessionRepository(connection)
			session, err := repository.CreateSession(ctx, "/work", "uuid", "")
			require.NoError(t, err)

			entry := appendTestMessage(ctx, t, repository, session.ID, nil, database.RoleUser, "hello")
			_, err = connection.ExecContext(ctx, test.query, test.args(t, session, entry)...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.wantError)
		})
	}
}

func invalidUUIDCases() []invalidUUIDCase {
	return []invalidUUIDCase{
		invalidSessionIDCase(),
		invalidEntryIDCase(),
		invalidMessageEntryIDCase(),
		invalidMessageIDCase(),
	}
}

func invalidSessionIDCase() invalidUUIDCase {
	return invalidUUIDCase{
		name: "session id trigger",
		query: `INSERT INTO sessions (id, cwd, name, parent_session, created_at, updated_at)
VALUES ('legacy-session', '/work', '', '', ?, ?)`,
		args: func(_ *testing.T, _ *database.SessionEntity, _ *database.EntryEntity) []any {
			now := time.Now().UTC().Format(time.RFC3339Nano)

			return []any{now, now}
		},
		wantError: "sessions.id must be a UUIDv7",
	}
}

func invalidEntryIDCase() invalidUUIDCase {
	return invalidUUIDCase{
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
		args: func(_ *testing.T, session *database.SessionEntity, _ *database.EntryEntity) []any {
			return []any{session.ID, time.Now().UTC().Format(time.RFC3339Nano)}
		},
		wantError: "session_entries.id must be a UUIDv7",
	}
}

func invalidMessageEntryIDCase() invalidUUIDCase {
	return invalidUUIDCase{
		name: "message entry id trigger",
		query: `INSERT INTO session_messages (
    id, session_id, entry_id, sender, role, content, provider, model, created_at
) VALUES (?, ?, 'legacy-entry', 'user', 'user', 'hello', '', '', ?)`,
		args: func(t *testing.T, session *database.SessionEntity, _ *database.EntryEntity) []any {
			t.Helper()

			return []any{testUUIDV7(t), session.ID, time.Now().UTC().Format(time.RFC3339Nano)}
		},
		wantError: "session_messages.entry_id must be a UUIDv7",
	}
}

func invalidMessageIDCase() invalidUUIDCase {
	return invalidUUIDCase{
		name: "message id trigger",
		query: `INSERT INTO session_messages (
    id, session_id, entry_id, sender, role, content, provider, model, created_at
) VALUES ('legacy-message', ?, ?, 'user', 'user', 'hello', '', '', ?)`,
		args: func(_ *testing.T, session *database.SessionEntity, entry *database.EntryEntity) []any {
			return []any{session.ID, entry.ID, time.Now().UTC().Format(time.RFC3339Nano)}
		},
		wantError: "session_messages.id must be a UUIDv7",
	}
}

func testUUIDV7(t *testing.T) string {
	t.Helper()

	id, err := uuid.NewV7()
	require.NoError(t, err)

	return id.String()
}
