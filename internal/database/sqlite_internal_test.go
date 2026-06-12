package database_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite" // register SQLite driver for connection tests.

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

func TestSQLiteDSNAddsPragmas(t *testing.T) {
	t.Parallel()

	dsn := database.SQLiteDSN("/tmp/librecode.db", database.SQLiteOptions{BusyTimeout: 15 * time.Second})

	assert.Contains(t, dsn, "file:///tmp/librecode.db?")
	assert.Contains(t, dsn, "_pragma=busy_timeout%3D15000")
	assert.Contains(t, dsn, "_pragma=journal_mode%3DWAL")
	assert.Contains(t, dsn, "_pragma=synchronous%3DNORMAL")
	assert.Contains(t, dsn, "_pragma=foreign_keys%3DON")
}

func TestConfigureSQLiteAppliesPragmas(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	connection := openTestSQLite(t, filepath.Join(t.TempDir(), "librecode.db"), 1500*time.Millisecond)
	require.NoError(t, database.ConfigureSQLite(
		ctx,
		connection,
		database.SQLiteOptions{BusyTimeout: 1500 * time.Millisecond},
	))

	assert.Equal(t, 1500, queryPragmaInt(t, connection, "busy_timeout"))
	assert.Equal(t, "wal", queryPragmaString(t, connection, "journal_mode"))
	assert.Equal(t, 1, queryPragmaInt(t, connection, "synchronous"))
	assert.Equal(t, 1, queryPragmaInt(t, connection, "foreign_keys"))
}

func openTestSQLite(t *testing.T, path string, busyTimeout time.Duration) *sql.DB {
	t.Helper()

	dsn := database.SQLiteDSN(path, database.SQLiteOptions{BusyTimeout: busyTimeout})
	connection, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	connection.SetMaxIdleConns(1)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })

	return connection
}

func queryPragmaInt(t *testing.T, db *sql.DB, name string) int {
	t.Helper()

	var value int
	require.NoError(t, db.QueryRowContext(context.Background(), "PRAGMA "+name).Scan(&value))

	return value
}

func queryPragmaString(t *testing.T, db *sql.DB, name string) string {
	t.Helper()

	var value string
	require.NoError(t, db.QueryRowContext(context.Background(), "PRAGMA "+name).Scan(&value))

	return value
}
