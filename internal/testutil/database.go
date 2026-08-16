package testutil

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

// OpenMemoryDatabase opens an isolated in-memory SQLite database, registers
// schema migrations, and closes it when the test finishes. The database name is
// derived from the test name, so subtests get independent databases while
// subtests sharing cache=shared keep a single connection.
func OpenMemoryDatabase(tb testing.TB) *sql.DB {
	tb.Helper()

	name := strings.ReplaceAll(tb.Name(), "/", "_")
	connection, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(tb, err)
	connection.SetMaxOpenConns(1)
	tb.Cleanup(func() { require.NoError(tb, connection.Close()) })
	require.NoError(tb, database.Migrate(tb.Context(), connection))

	return connection
}

// CreateSession creates a session with the test binary as its working
// directory, or fails the test.
func CreateSession(
	tb testing.TB,
	sessions *database.SessionRepository,
	name string,
) *database.SessionEntity {
	tb.Helper()

	session, err := sessions.CreateSession(tb.Context(), tb.TempDir(), name, "")
	require.NoError(tb, err)

	return session
}

// CreateChildSession creates a session linked to a parent session, or fails
// the test.
func CreateChildSession(
	tb testing.TB,
	sessions *database.SessionRepository,
	name string,
	parentID string,
) *database.SessionEntity {
	tb.Helper()

	session, err := sessions.CreateSession(tb.Context(), tb.TempDir(), name, parentID)
	require.NoError(tb, err)

	return session
}
