package database_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
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
		waitGroup.Go(func() {
			for entryIndex := range 20 {
				_, appendErr := repository.AppendMessage(ctx, session.ID, nil, &database.MessageEntity{
					Timestamp: time.Now().UTC(),
					Role:      database.RoleUser,
					Content:   strings.Repeat("x", writerIndex+entryIndex+1),
					Provider:  "",
					Model:     "", Parts: nil,
				})
				appendErrors <- appendErr
			}
		})
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

func TestSessionRepositoryConcurrentCompactionsChooseOneWinner(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping SQLite compaction contention test in short mode")
	}

	t.Parallel()

	ctx := context.Background()
	dbs := openMigratedSQLitePair(ctx, t, 2*time.Second)
	repositories := []*database.SessionRepository{
		database.NewSessionRepository(dbs.primary),
		database.NewSessionRepository(dbs.secondary),
	}
	session, err := repositories[0].CreateSession(ctx, t.TempDir(), "compaction race", "")
	require.NoError(t, err)
	parent, err := repositories[0].AppendMessage(ctx, session.ID, nil, &database.MessageEntity{
		Timestamp: time.Now().UTC(),
		Role:      database.RoleUser,
		Content:   compactionTestHistory,
		Provider:  "",
		Model:     "", Parts: nil,
	})
	require.NoError(t, err)

	start := make(chan struct{})
	results := make(chan compactionAppendResult, len(repositories))

	var waitGroup sync.WaitGroup
	for index, repository := range repositories {
		waitGroup.Go(func() {
			<-start

			entry, appendErr := repository.AppendCompaction(ctx, newCompactionRaceInput(
				session.ID, parent.ID, uuid.Must(uuid.NewV7()).String(), fmt.Sprintf("summary %d", index),
			))
			results <- compactionAppendResult{entry: entry, err: appendErr}
		})
	}

	close(start)
	waitGroup.Wait()
	close(results)

	successes := 0
	stale := 0

	for result := range results {
		if result.err == nil {
			successes++

			require.NotNil(t, result.entry)

			continue
		}

		if errors.Is(result.err, database.ErrStaleCompactionParent) {
			assertOopsCode(t, result.err, "stale_compaction_parent")

			stale++

			continue
		}

		require.NoError(t, result.err)
	}

	assert.Equal(t, 1, successes)
	assert.Equal(t, 1, stale)

	children, err := repositories[0].Children(ctx, session.ID, &parent.ID)
	require.NoError(t, err)
	require.Len(t, children, 1)
	assert.Equal(t, database.EntryTypeCompaction, children[0].Type)
}

func TestSessionRepositoryCompactionOperationIsIdempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping SQLite compaction idempotency test in short mode")
	}

	t.Parallel()

	ctx := context.Background()
	dbs := openMigratedSQLitePair(ctx, t, 2*time.Second)
	primary := database.NewSessionRepository(dbs.primary)
	secondary := database.NewSessionRepository(dbs.secondary)
	session, err := primary.CreateSession(ctx, t.TempDir(), "compaction retry", "")
	require.NoError(t, err)
	parent, err := primary.AppendMessage(ctx, session.ID, nil, &database.MessageEntity{
		Timestamp: time.Now().UTC(),
		Role:      database.RoleUser,
		Content:   compactionTestHistory,
		Provider:  "",
		Model:     "", Parts: nil,
	})
	require.NoError(t, err)

	operationID := uuid.Must(uuid.NewV7()).String()

	first, err := primary.AppendCompaction(ctx, newCompactionRaceInput(session.ID, parent.ID, operationID, "summary"))
	require.NoError(t, err)
	second, err := secondary.AppendCompaction(
		ctx,
		newCompactionRaceInput(session.ID, parent.ID, operationID, "summary"),
	)
	require.NoError(t, err)

	assert.Equal(t, first.ID, second.ID)

	otherParent, err := primary.AppendMessage(ctx, session.ID, &parent.ID, &database.MessageEntity{
		Timestamp: time.Now().UTC(),
		Role:      database.RoleUser,
		Content:   "different branch target",
		Provider:  "",
		Model:     "", Parts: nil,
	})
	require.NoError(t, err)
	mismatched, err := secondary.AppendCompaction(
		ctx,
		newCompactionRaceInput(session.ID, otherParent.ID, operationID, "summary"),
	)
	require.Error(t, err)
	assert.Nil(t, mismatched)
	assertOopsCode(t, err, "compaction_operation_mismatch")

	otherChildren, err := primary.Children(ctx, session.ID, &otherParent.ID)
	require.NoError(t, err)
	assert.Empty(t, otherChildren)

	children, err := primary.Children(ctx, session.ID, &parent.ID)
	require.NoError(t, err)
	assert.Len(t, children, 2)
	assert.Equal(t, database.EntryTypeCompaction, children[0].Type)
}

func assertOopsCode(t *testing.T, err error, wantCode string) {
	t.Helper()

	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, wantCode, oopsErr.Code())
}

type compactionAppendResult struct {
	entry *database.EntryEntity
	err   error
}

func newCompactionRaceInput(sessionID, parentID, operationID, summary string) *database.AppendCompactionInput {
	return &database.AppendCompactionInput{
		ParentID:         &parentID,
		Details:          nil,
		SessionID:        sessionID,
		Summary:          summary,
		FirstKeptEntryID: parentID,
		TokensBefore:     100,
		FromHook:         false,
		OperationID:      operationID,
	}
}

func TestSQLiteBusyTimeoutWaitsForExternalWriter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping SQLite lock contention test in short mode")
	}

	t.Parallel()

	ctx := context.Background()
	dbs := openMigratedSQLitePair(ctx, t, 2*time.Second)
	withSessionsTableLock(ctx, t, dbs.primary, func(lock *sql.Tx) {
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
	})
}

func TestSQLiteShortBusyTimeoutStillReportsBusy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping SQLite lock contention test in short mode")
	}

	t.Parallel()

	ctx := context.Background()
	dbs := openMigratedSQLitePair(ctx, t, 10*time.Millisecond)
	withSessionsTableLock(ctx, t, dbs.primary, func(_ *sql.Tx) {
		_, err := database.NewSessionRepository(dbs.secondary).CreateSession(ctx, t.TempDir(), "blocked", "")
		require.Error(t, err)
		require.True(t, isSQLiteBusyError(err), "expected busy error, got %v", err)
	})
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

func withSessionsTableLock(ctx context.Context, t *testing.T, connection *sql.DB, callback func(*sql.Tx)) {
	t.Helper()

	lock, err := connection.BeginTx(ctx, nil)
	require.NoError(t, err)

	defer func() {
		rollbackErr := lock.Rollback()
		if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			require.NoError(t, rollbackErr)
		}
	}()

	_, err = lock.ExecContext(ctx, `UPDATE sessions SET updated_at = updated_at`)
	require.NoError(t, err)

	callback(lock)
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
