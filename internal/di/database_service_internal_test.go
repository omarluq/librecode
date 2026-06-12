package di

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/config"
)

func TestOpenSQLiteDatabaseAppliesPragmasAndMigrations(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "librecode.db")
	connection, err := openSQLiteDatabase(databasePath, config.DatabaseConfig{
		Path:            "",
		ApplyMigrations: true,
		MaxOpenConns:    1,
		MaxIdleConns:    1,
		ConnMaxLifetime: 0,
		BusyTimeout:     1200 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })

	assert.Equal(t, 1200, queryDatabasePragmaInt(t, connection, "busy_timeout"))
	assert.Equal(t, "wal", queryDatabasePragmaString(t, connection, "journal_mode"))
	assert.Equal(t, 1, queryDatabasePragmaInt(t, connection, "synchronous"))
	assert.Equal(t, 1, queryDatabasePragmaInt(t, connection, "foreign_keys"))

	var tableName string
	err = connection.QueryRowContext(
		context.Background(),
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'sessions'`,
	).Scan(&tableName)
	require.NoError(t, err)
	assert.Equal(t, "sessions", tableName)
}

func TestDatabaseServiceHealthCheckAndShutdown(t *testing.T) {
	t.Parallel()

	connection, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "librecode.db"))
	require.NoError(t, err)
	service := &DatabaseService{DB: connection, Sessions: nil, Documents: nil, path: ""}

	require.NoError(t, service.HealthCheck(context.Background()))
	require.NoError(t, service.Shutdown(context.Background()))
	assert.Error(t, service.HealthCheck(context.Background()))
}

func TestDatabaseServiceShutdownReturnsContextError(t *testing.T) {
	t.Parallel()

	connection, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "librecode.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	service := &DatabaseService{DB: connection, Sessions: nil, Documents: nil, path: ""}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	assert.ErrorIs(t, service.Shutdown(ctx), context.Canceled)
}

func TestCloseAfterSetupErrorPreservesSetupAndCloseErrors(t *testing.T) {
	t.Parallel()

	connection, err := sql.Open(registerCloseErrorDriver(), "")
	require.NoError(t, err)
	connection.SetMaxIdleConns(1)
	require.NoError(t, connection.PingContext(context.Background()))
	setupErr := errors.New("setup failed")

	err = closeAfterSetupError(connection, "close_after_setup", "setup", "/tmp/librecode.db", setupErr)
	require.Error(t, err)
	assert.ErrorIs(t, err, setupErr)
	assert.ErrorIs(t, err, errCloseFailed)
	assert.ErrorContains(t, err, "/tmp/librecode.db")
}

func TestResolveDatabasePath(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LIBRECODE_HOME", filepath.Join(home, ".librecode"))
	t.Chdir(cwd)

	projectDatabasePath := filepath.Join(cwd, ".librecode", "librecode.db")
	require.NoError(t, os.MkdirAll(filepath.Dir(projectDatabasePath), 0o700))
	require.NoError(t, os.WriteFile(projectDatabasePath, []byte(""), 0o600))

	resolved, err := resolveDatabasePath(testDatabasePathConfig(""))
	require.NoError(t, err)
	assert.Equal(t, projectDatabasePath, resolved)

	resolved, err = resolveDatabasePath(testDatabasePathConfig("~/custom.db"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "custom.db"), resolved)

	resolved, err = resolveDatabasePath(testDatabasePathConfig("/tmp/custom.db"))
	require.NoError(t, err)
	assert.Equal(t, "/tmp/custom.db", resolved)
}

func testDatabasePathConfig(path string) *config.Config {
	cfg := config.Load("").MustGet()
	cfg.Database.Path = path

	return cfg
}

func queryDatabasePragmaInt(t *testing.T, db *sql.DB, name string) int {
	t.Helper()

	var value int
	require.NoError(t, db.QueryRowContext(context.Background(), "PRAGMA "+name).Scan(&value))

	return value
}

func queryDatabasePragmaString(t *testing.T, db *sql.DB, name string) string {
	t.Helper()

	var value string
	require.NoError(t, db.QueryRowContext(context.Background(), "PRAGMA "+name).Scan(&value))

	return value
}

var (
	closeDriverIndex int
	closeDriverMu    sync.Mutex
	errCloseFailed   = errors.New("close failed")
)

func registerCloseErrorDriver() string {
	closeDriverMu.Lock()
	defer closeDriverMu.Unlock()
	closeDriverIndex++
	driverName := "close-error-sqlite-test-" + strconv.Itoa(closeDriverIndex)
	sql.Register(driverName, closeErrorDriver{})

	return driverName
}

type closeErrorDriver struct{}

func (closeErrorDriver) Open(_ string) (driver.Conn, error) {
	return closeErrorConn{}, nil
}

type closeErrorConn struct{}

func (closeErrorConn) Prepare(_ string) (driver.Stmt, error) {
	return nil, errors.New("prepare unsupported")
}
func (closeErrorConn) Close() error                       { return errCloseFailed }
func (closeErrorConn) Begin() (driver.Tx, error)          { return nil, errors.New("begin unsupported") }
func (closeErrorConn) Ping(context.Context) error         { return nil }
func (closeErrorConn) ResetSession(context.Context) error { return nil }
func (closeErrorConn) IsValid() bool                      { return true }

var _ driver.Pinger = closeErrorConn{}
var _ driver.SessionResetter = closeErrorConn{}
var _ driver.Validator = closeErrorConn{}
var _ io.Closer = closeErrorConn{}
