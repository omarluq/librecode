package database_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

type sqlitePair struct {
	primary   *sql.DB
	secondary *sql.DB
}

func TestSessionRepositoryConcurrentWritersWaitForBusyDatabase(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping SQLite contention test in short mode")
	}
	t.Parallel()

	ctx := context.Background()
	dbs := openMigratedSQLitePair(ctx, t, 2*time.Second)
	primaryRepository := database.NewSessionRepository(dbs.primary)
	secondaryRepository := database.NewSessionRepository(dbs.secondary)
	session, err := primaryRepository.CreateSession(ctx, t.TempDir(), "concurrent", "")
	require.NoError(t, err)

	var waitGroup sync.WaitGroup
	appendErrors := make(chan error, 40)
	for writerIndex, repository := range []*database.SessionRepository{primaryRepository, secondaryRepository} {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for entryIndex := range 20 {
				_, appendErr := repository.AppendMessage(ctx, session.ID, nil, &database.MessageEntity{
					Timestamp: time.Now().UTC(),
					Role:      database.RoleUser,
					Content:   strings.Repeat("x", writerIndex+entryIndex+1),
					Provider:  "",
					Model:     "",
				})
				appendErrors <- appendErr
			}
		}()
	}
	waitGroup.Wait()
	close(appendErrors)
	for appendErr := range appendErrors {
		require.NoError(t, appendErr)
	}

	entries, err := primaryRepository.Entries(ctx, session.ID)
	require.NoError(t, err)
	require.Len(t, entries, 40)
}

func TestSQLiteBusyTimeoutWaitsForExternalWriter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping SQLite lock contention test in short mode")
	}
	t.Parallel()

	ctx := context.Background()
	dbs := openMigratedSQLitePair(ctx, t, 2*time.Second)
	lock := lockSessionsTable(ctx, t, dbs.primary)

	insertDone := make(chan error, 1)
	go func() {
		_, createErr := database.NewSessionRepository(dbs.secondary).CreateSession(ctx, t.TempDir(), "waiter", "")
		insertDone <- createErr
	}()

	select {
	case err := <-insertDone:
		require.FailNowf(t, "writer completed before lock release", "error: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	require.NoError(t, lock.Commit())
	require.NoError(t, <-insertDone)
}

func TestSQLiteShortBusyTimeoutStillReportsBusy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping SQLite lock contention test in short mode")
	}
	t.Parallel()

	ctx := context.Background()
	dbs := openMigratedSQLitePair(ctx, t, 10*time.Millisecond)
	lock := lockSessionsTable(ctx, t, dbs.primary)
	t.Cleanup(func() { require.NoError(t, lock.Rollback()) })

	_, err := database.NewSessionRepository(dbs.secondary).CreateSession(ctx, t.TempDir(), "blocked", "")
	require.Error(t, err)
	require.True(t, isSQLiteBusyError(err), "expected busy error, got %v", err)
}

func openMigratedSQLitePair(ctx context.Context, t *testing.T, busyTimeout time.Duration) sqlitePair {
	t.Helper()

	databasePath := filepath.Join(t.TempDir(), "librecode.db")
	primary := openTestSQLite(t, databasePath, busyTimeout)
	require.NoError(t, database.ConfigureSQLite(ctx, primary, database.SQLiteOptions{BusyTimeout: busyTimeout}))
	require.NoError(t, database.Migrate(ctx, primary))
	secondary := openTestSQLite(t, databasePath, busyTimeout)
	require.NoError(t, database.ConfigureSQLite(ctx, secondary, database.SQLiteOptions{BusyTimeout: busyTimeout}))

	return sqlitePair{primary: primary, secondary: secondary}
}

func lockSessionsTable(ctx context.Context, t *testing.T, connection *sql.DB) *sql.Tx {
	t.Helper()

	lock, err := connection.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = lock.ExecContext(ctx, `UPDATE sessions SET updated_at = updated_at`)
	require.NoError(t, err)

	return lock
}

func isSQLiteBusyError(err error) bool {
	for current := err; current != nil; current = errors.Unwrap(current) {
		message := strings.ToLower(current.Error())
		if strings.Contains(message, "busy") || strings.Contains(message, "locked") {
			return true
		}
	}

	return false
}
