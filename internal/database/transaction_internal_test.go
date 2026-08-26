package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vingarcia/ksql"
)

const (
	contentionBusy         = "busy"
	contentionBusySnapshot = "busy_snapshot"
	contentionLocked       = "locked"
)

func TestIsSQLiteBusy(t *testing.T) {
	t.Parallel()

	busy := sqliteContentionError(t, contentionBusy)
	busySnapshot := sqliteContentionError(t, contentionBusySnapshot)
	locked := sqliteContentionError(t, contentionLocked)

	tests := []struct {
		err  error
		name string
		want bool
	}{
		{name: "busy", err: busy, want: true},
		{name: "wrapped busy", err: fmt.Errorf("wrapped: %w", busy), want: true},
		{name: "oops wrapped busy snapshot", err: oops.In("database").Wrapf(busySnapshot, "snapshot"), want: true},
		{name: contentionLocked, err: locked, want: false},
		{name: "unrelated", err: errors.New("busy database text is not a result code"), want: false},
		{name: "nil", err: nil, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.want, isSQLiteBusy(test.err))
		})
	}
}

func TestTransactionProviderRetriesBusyAttemptBoundaries(t *testing.T) {
	t.Parallel()

	busy := sqliteContentionError(t, contentionBusy)
	tests := []struct {
		firstAttempt     func(func(ksql.Provider) error) error
		name             string
		busyFromCallback bool
		wantCallbacks    int
	}{
		{
			name: "begin", firstAttempt: func(func(ksql.Provider) error) error { return busy },
			busyFromCallback: false, wantCallbacks: 1,
		},
		{
			name: "callback SQL", firstAttempt: func(callback func(ksql.Provider) error) error {
				return callback(nil)
			}, busyFromCallback: true, wantCallbacks: 2,
		},
		{
			name: "commit", firstAttempt: func(callback func(ksql.Provider) error) error {
				if err := callback(nil); err != nil {
					return err
				}

				return busy
			}, busyFromCallback: false, wantCallbacks: 2,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			attempts := 0
			callbacks := 0
			provider := &transactionProvider{
				Provider: nil, connection: nil, waitRetry: nil, restoreReadWrite: nil, diagnostic: nil,
				executeAttempt: func(
					_ context.Context, _ *sql.TxOptions, callback func(ksql.Provider) error,
				) error {
					attempts++
					if attempts == 1 {
						return test.firstAttempt(callback)
					}

					return callback(nil)
				},
			}

			err := provider.Transaction(t.Context(), func(_ ksql.Provider) error {
				callbacks++
				if test.busyFromCallback && callbacks == 1 {
					return busy
				}

				return nil
			})

			require.NoError(t, err)
			assert.Equal(t, 2, attempts)
			assert.Equal(t, test.wantCallbacks, callbacks)
		})
	}
}

func TestReadOnlyTransactionValueUsesGenericProviderTransaction(t *testing.T) {
	t.Parallel()

	provider := new(ksql.Mock)
	transactionCalls := 0
	provider.TransactionFn = func(_ context.Context, callback func(ksql.Provider) error) error {
		transactionCalls++

		return callback(provider)
	}

	result, err := readOnlyTransactionValue(t.Context(), provider, func(ksql.Provider) (string, error) {
		return "snapshot", nil
	})

	require.NoError(t, err)
	assert.Equal(t, "snapshot", result.value)
	assert.Equal(t, 1, transactionCalls)
}

func TestTransactionValueDiscardsCommitFailureResult(t *testing.T) {
	t.Parallel()

	busy := sqliteContentionError(t, contentionBusy)
	attempts := 0
	provider := &transactionProvider{
		Provider: nil, connection: nil, waitRetry: nil, restoreReadWrite: nil, diagnostic: nil,
		executeAttempt: func(
			_ context.Context, _ *sql.TxOptions, callback func(ksql.Provider) error,
		) error {
			attempts++

			if err := callback(nil); err != nil {
				return err
			}

			return busy
		},
	}

	result, err := transactionValue(t.Context(), provider, func(ksql.Provider) (string, error) {
		return fmt.Sprintf("attempt-%d", attempts), nil
	})

	require.Error(t, err)
	assert.Empty(t, result)
	assert.Equal(t, sqliteTransactionAttempts, attempts)
}

func TestTransactionProviderRetriesWholeCallback(t *testing.T) {
	t.Parallel()

	connection := openInternalSQLite(t, filepath.Join(t.TempDir(), "retry.db"), 0, true)
	require.NoError(t, Migrate(t.Context(), connection))
	provider := internalTransactionProvider(t, connection)
	busy := sqliteContentionError(t, contentionBusy)

	callbacks := 0
	err := provider.Transaction(t.Context(), func(transaction ksql.Provider) error {
		callbacks++
		if callbacks == 1 {
			_, execErr := transaction.Exec(t.Context(), `INSERT INTO runtime_documents
(namespace, document_key, value_json, updated_at)
VALUES ('retry', 'rolled-back', 'value', '2026-01-01T00:00:00Z')`)
			require.NoError(t, execErr)

			return busy
		}

		_, execErr := transaction.Exec(t.Context(), `INSERT INTO runtime_documents
(namespace, document_key, value_json, updated_at)
VALUES ('retry', 'committed', 'value', '2026-01-01T00:00:00Z')`)
		if execErr != nil {
			return fmt.Errorf("insert committed document: %w", execErr)
		}

		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 2, callbacks)
	assert.Equal(t, 0, countInternalRows(
		t, connection, "SELECT COUNT(*) FROM runtime_documents WHERE document_key = 'rolled-back'",
	))
	assert.Equal(t, 1, countInternalRows(
		t, connection, "SELECT COUNT(*) FROM runtime_documents WHERE document_key = 'committed'",
	))
}

func TestTransactionValueDiscardsFailedAttemptResult(t *testing.T) {
	t.Parallel()

	connection := openInternalSQLite(t, filepath.Join(t.TempDir(), "result.db"), 0, true)
	provider := internalTransactionProvider(t, connection)
	busy := sqliteContentionError(t, contentionBusy)
	attempts := 0

	result, err := transactionValue(t.Context(), provider, func(ksql.Provider) (string, error) {
		attempts++
		if attempts == 1 {
			return "rolled-back", busy
		}

		return "committed", nil
	})

	require.NoError(t, err)
	assert.Equal(t, "committed", result.value)
	assert.Equal(t, 2, attempts)
}

func TestClassifyTransactionOutcome(t *testing.T) {
	t.Parallel()

	busy := sqliteContentionError(t, contentionBusy)
	locked := sqliteContentionError(t, contentionLocked)
	tests := []struct {
		err      error
		name     string
		want     transactionOutcome
		attempts int
	}{
		{name: string(transactionOutcomeSuccess), err: nil, want: transactionOutcomeSuccess, attempts: 0},
		{
			name: "busy exhaustion", err: busy, attempts: sqliteTransactionAttempts,
			want: transactionOutcomeBusyExhausted,
		},
		{name: contentionLocked, err: locked, attempts: 1, want: transactionOutcomeLocked},
		{name: string(TaskCanceled), err: context.Canceled, attempts: 1, want: transactionOutcomeCanceled},
		{name: "deadline", err: context.DeadlineExceeded, attempts: 1, want: transactionOutcomeDeadline},
		{name: "domain error", err: errors.New("domain"), attempts: 1, want: transactionOutcomeError},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.want, classifyTransactionOutcome(test.err, test.attempts))
		})
	}
}

type transactionDiagnosticTest struct {
	attemptError    error
	waitError       error
	wantError       error
	name            string
	wantOutcome     transactionOutcome
	wantPhase       transactionPhase
	wantAttempts    int
	wantPrimaryCode int
	preCanceled     bool
}

func TestTransactionProviderDiagnostics(t *testing.T) {
	t.Parallel()

	busy := sqliteContentionError(t, contentionBusy)
	locked := sqliteContentionError(t, contentionLocked)
	tests := []transactionDiagnosticTest{
		{
			attemptError: nil, waitError: nil, wantError: nil, name: string(transactionOutcomeSuccess),
			wantOutcome: transactionOutcomeSuccess, wantPhase: transactionPhaseAttempt,
			wantAttempts: 1, wantPrimaryCode: 0, preCanceled: false,
		},
		{
			attemptError: busy, waitError: nil, wantError: busy, name: "busy exhaustion",
			wantOutcome: transactionOutcomeBusyExhausted, wantPhase: transactionPhaseAttempt,
			wantAttempts: sqliteTransactionAttempts, wantPrimaryCode: sqlitePrimaryCode(busy), preCanceled: false,
		},
		{
			attemptError: locked, waitError: nil, wantError: locked, name: contentionLocked,
			wantOutcome: transactionOutcomeLocked, wantPhase: transactionPhaseAttempt,
			wantAttempts: 1, wantPrimaryCode: sqlitePrimaryCode(locked), preCanceled: false,
		},
		{
			attemptError: context.DeadlineExceeded, waitError: nil, wantError: context.DeadlineExceeded,
			name: "deadline during attempt", wantOutcome: transactionOutcomeDeadline,
			wantPhase: transactionPhaseAttempt, wantAttempts: 1, wantPrimaryCode: 0, preCanceled: false,
		},
		{
			attemptError: busy, waitError: context.Canceled, wantError: context.Canceled,
			name: "retry wait cancellation", wantOutcome: transactionOutcomeCanceled,
			wantPhase: transactionPhaseRetryWait, wantAttempts: 1, wantPrimaryCode: 0, preCanceled: false,
		},
		{
			attemptError: nil, waitError: nil, wantError: context.Canceled, name: "pre-canceled",
			wantOutcome: transactionOutcomeCanceled, wantPhase: transactionPhaseAttempt,
			wantAttempts: 0, wantPrimaryCode: 0, preCanceled: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			testTransactionProviderDiagnostic(t, &test)
		})
	}
}

func testTransactionProviderDiagnostic(t *testing.T, test *transactionDiagnosticTest) {
	t.Helper()

	ctx := t.Context()
	if test.preCanceled {
		canceledCtx, cancel := context.WithCancel(ctx)
		cancel()

		ctx = canceledCtx
	}

	attempts := 0

	var diagnostic transactionDiagnostic

	captured := false
	provider := &transactionProvider{
		Provider:   nil,
		connection: nil,
		executeAttempt: func(
			_ context.Context, _ *sql.TxOptions, _ func(ksql.Provider) error,
		) error {
			attempts++

			return test.attemptError
		},
		waitRetry:        func(context.Context, time.Duration) error { return test.waitError },
		restoreReadWrite: nil,
		diagnostic: func(value transactionDiagnostic) {
			diagnostic = value
			captured = true
		},
	}

	err := provider.Transaction(ctx, func(ksql.Provider) error { return nil })
	if test.wantError == nil {
		require.NoError(t, err)
	} else {
		require.ErrorIs(t, err, test.wantError)
	}

	assert.True(t, captured)
	assert.Equal(t, transactionModeWrite, diagnostic.mode)
	assert.Equal(t, test.wantOutcome, diagnostic.outcome)
	assert.Equal(t, test.wantPhase, diagnostic.phase)
	assert.Equal(t, test.wantAttempts, diagnostic.attempts)
	assert.Equal(t, test.wantAttempts, attempts)
	assert.GreaterOrEqual(t, diagnostic.retryDelay, time.Duration(0))
	assert.GreaterOrEqual(t, diagnostic.attemptDuration, time.Duration(0))
	assert.Equal(t, test.wantPrimaryCode, diagnostic.primaryCode)
}

func TestTransactionProviderTerminalErrors(t *testing.T) {
	t.Parallel()

	connection := openInternalSQLite(t, filepath.Join(t.TempDir(), "terminal.db"), 0, true)
	provider := internalTransactionProvider(t, connection)
	busy := sqliteContentionError(t, contentionBusy)
	locked := sqliteContentionError(t, contentionLocked)
	domain := errors.New("domain failure")

	tests := []struct {
		err          error
		name         string
		wantAttempts int
		wantExhaust  bool
	}{
		{name: "busy exhausts", err: busy, wantAttempts: sqliteTransactionAttempts, wantExhaust: true},
		{name: contentionLocked + " does not retry", err: locked, wantAttempts: 1, wantExhaust: false},
		{name: "domain does not retry", err: domain, wantAttempts: 1, wantExhaust: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			attempts := 0
			err := provider.Transaction(t.Context(), func(ksql.Provider) error {
				attempts++

				return test.err
			})

			require.Error(t, err)
			assert.Equal(t, test.wantAttempts, attempts)
			assert.Equal(t, test.wantExhaust, errors.Is(err, busy))

			if test.wantExhaust {
				assert.ErrorContains(t, err, "exhausted after 3 attempts")
			}
		})
	}
}

func TestTransactionProviderCancellationStopsRetry(t *testing.T) {
	t.Parallel()

	connection := openInternalSQLite(t, filepath.Join(t.TempDir(), "cancel.db"), 0, true)
	provider := internalTransactionProvider(t, connection)
	busy := sqliteContentionError(t, contentionBusy)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	attempts := 0

	var diagnostic transactionDiagnostic

	provider.diagnostic = func(value transactionDiagnostic) { diagnostic = value }

	err := provider.Transaction(ctx, func(ksql.Provider) error {
		attempts++

		cancel()

		return busy
	})

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, attempts)
	assert.Equal(t, transactionOutcomeCanceled, diagnostic.outcome)
	assert.Equal(t, transactionPhaseRetryWait, diagnostic.phase)
}

func TestReadOnlyTransactionCancellationRestoresWrites(t *testing.T) {
	t.Parallel()

	connection := openInternalSQLite(t, filepath.Join(t.TempDir(), "read-only-cancel.db"), 0, true)
	provider := internalTransactionProvider(t, connection)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	err := provider.readOnlyTransaction(ctx, func(ksql.Provider) error {
		cancel()

		return ctx.Err()
	})
	require.ErrorIs(t, err, context.Canceled)

	_, err = connection.ExecContext(t.Context(), "CREATE TABLE writable_after_cancel (id INTEGER)")
	require.NoError(t, err)
}

func TestReadOnlyTransactionPanicRestoresWrites(t *testing.T) {
	t.Parallel()

	connection := openInternalSQLite(t, filepath.Join(t.TempDir(), "read-only-panic.db"), 0, true)
	provider := internalTransactionProvider(t, connection)
	panicValue := "callback panic"

	func() {
		defer func() { assert.Equal(t, panicValue, recover()) }()

		err := provider.readOnlyTransaction(t.Context(), func(ksql.Provider) error {
			panic(panicValue)
		})
		require.NoError(t, err)
	}()

	_, err := connection.ExecContext(t.Context(), "CREATE TABLE writable_after_panic (id INTEGER)")
	require.NoError(t, err)
}

func TestReadOnlyTransactionRestoreFailureDiscardsConnection(t *testing.T) {
	t.Parallel()

	connection := openInternalSQLite(t, filepath.Join(t.TempDir(), "read-only-restore.db"), 0, true)
	provider := internalTransactionProvider(t, connection)
	restoreFailure := errors.New("restore query_only")

	var poisoned any

	provider.restoreReadWrite = func(_ context.Context, conn *sql.Conn) error {
		require.NoError(t, conn.Raw(func(driverConnection any) error {
			poisoned = driverConnection

			return nil
		}))

		return restoreFailure
	}

	err := provider.readOnlyTransaction(t.Context(), func(ksql.Provider) error { return nil })

	require.ErrorIs(t, err, restoreFailure)

	fresh, err := connection.Conn(t.Context())
	require.NoError(t, err)

	defer func() { require.NoError(t, fresh.Close()) }()

	require.NoError(t, fresh.Raw(func(driverConnection any) error {
		assert.NotSame(t, poisoned, driverConnection)

		return nil
	}))
	_, err = fresh.ExecContext(t.Context(), "CREATE TABLE writable_after_restore_failure (id INTEGER)")
	require.NoError(t, err)
}

func TestReadOnlyTransactionCanceledConnectionAcquisition(t *testing.T) {
	t.Parallel()

	connection := openInternalSQLite(t, filepath.Join(t.TempDir(), "read-only-acquire-cancel.db"), 0, true)
	provider := internalTransactionProvider(t, connection)
	pinned, err := connection.Conn(t.Context())
	require.NoError(t, err)

	defer func() { require.NoError(t, pinned.Close()) }()

	ctx, cancel := context.WithCancel(t.Context())

	var called atomic.Bool

	done := make(chan error, 1)

	go func() {
		done <- provider.readOnlyTransaction(ctx, func(ksql.Provider) error {
			called.Store(true)

			return nil
		})
	}()

	require.Eventually(t, func() bool { return connection.Stats().WaitCount > 0 }, time.Second, time.Millisecond)
	cancel()

	select {
	case err = <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("read-only connection acquisition did not honor cancellation")
	}

	assert.False(t, called.Load())
}

func TestTransactionAttemptErrorPreservesCauseAndCleanup(t *testing.T) {
	t.Parallel()

	cause := errors.New("original cause")
	cleanup := errors.New("cleanup failure")
	err := joinTransactionErrors(cause, cleanup)

	require.ErrorIs(t, err, cause)
	require.ErrorIs(t, err, cleanup)
}

func TestImmediateTransactionArbitratesBeforeReading(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "immediate.db")
	first := openInternalSQLite(t, path, 0, true)
	second := openInternalSQLite(t, path, 0, true)
	require.NoError(t, Migrate(t.Context(), first))

	transaction, err := first.BeginTx(t.Context(), nil)
	require.NoError(t, err)

	defer func() {
		rollbackErr := transaction.Rollback()
		require.True(t, rollbackErr == nil || errors.Is(rollbackErr, sql.ErrTxDone))
	}()

	_, err = second.ExecContext(t.Context(), "UPDATE sessions SET updated_at = updated_at")
	require.Error(t, err)
	assert.True(t, isSQLiteBusy(err))

	var count int
	require.NoError(t, transaction.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM sessions").Scan(&count))
	_, err = transaction.ExecContext(t.Context(), "UPDATE sessions SET updated_at = updated_at")
	require.NoError(t, err, "the transaction acquired the writer slot before taking its read snapshot")
	require.NoError(t, transaction.Commit())
}

func TestReadOnlyTransactionOverlapsImmediateWriterAndRejectsWrites(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "read-only.db")
	readerDB := openInternalSQLite(t, path, time.Second, true)
	writerDB := openInternalSQLite(t, path, time.Second, true)
	require.NoError(t, Migrate(t.Context(), readerDB))
	reader := internalTransactionProvider(t, readerDB)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	readerCtx, readerCancel := context.WithCancel(context.WithoutCancel(ctx))
	defer readerCancel()

	started := make(chan struct{})
	release := make(chan struct{})

	var releaseOnce sync.Once

	releaseReader := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseReader)

	var writeAccepted atomic.Bool

	readDone := make(chan error, 1)
	go func() {
		readDone <- reader.readOnlyTransaction(readerCtx, func(transaction ksql.Provider) error {
			var rows []struct {
				Count int `ksql:"count"`
			}
			if err := transaction.Query(readerCtx, &rows, "SELECT COUNT(*) AS count FROM sessions"); err != nil {
				return fmt.Errorf("query session count: %w", err)
			}

			close(started)
			<-release

			_, execErr := transaction.Exec(readerCtx, `INSERT INTO sessions
(id, parent_session_id, cwd, name, created_at, updated_at) VALUES ('read-only', NULL, '/', 'no', 'x', 'x')`)
			if execErr == nil {
				writeAccepted.Store(true)

				return nil
			}

			return fmt.Errorf("write in read-only transaction: %w", execErr)
		})
	}()

	select {
	case <-started:
	case err := <-readDone:
		require.NoError(t, err, "read-only transaction failed before establishing its snapshot")
	case <-ctx.Done():
		require.NoError(t, ctx.Err(), "timed out waiting for read-only transaction")
	}

	writer, err := writerDB.BeginTx(ctx, nil)
	require.NoError(t, err, "read-only transaction must not reserve the writer slot")

	defer func() {
		rollbackErr := writer.Rollback()
		require.True(t, rollbackErr == nil || errors.Is(rollbackErr, sql.ErrTxDone))
	}()

	_, err = writer.ExecContext(ctx, `UPDATE sessions SET updated_at = updated_at`)
	require.NoError(t, err)
	require.NoError(t, writer.Commit())
	releaseReader()

	select {
	case err = <-readDone:
	case <-ctx.Done():
		require.NoError(t, ctx.Err(), "timed out waiting for read-only transaction cleanup")
	}

	assert.False(t, writeAccepted.Load(), "read-only transaction accepted a write")
	require.Error(t, err)
	assert.False(t, isSQLiteBusy(err))
	assert.Equal(t, 0, countInternalRows(t, readerDB, "SELECT COUNT(*) FROM sessions WHERE id = 'read-only'"))
}

func internalTransactionProvider(t *testing.T, database *sql.DB) *transactionProvider {
	t.Helper()

	provider, err := newSQLProviderFromOpenConnection(database)
	require.NoError(t, err)

	return provider
}

func openInternalSQLite(t *testing.T, path string, timeout time.Duration, immediate bool) *sql.DB {
	t.Helper()

	dsn := SQLiteDSN(path, SQLiteOptions{BusyTimeout: timeout})
	if !immediate {
		dsn = filepath.ToSlash("file:" + path + "?_pragma=journal_mode%3DWAL&_pragma=busy_timeout%3D0&_txlock=deferred")
	}

	database, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	t.Cleanup(func() { require.NoError(t, database.Close()) })

	return database
}

func countInternalRows(t *testing.T, database *sql.DB, query string) int {
	t.Helper()

	var count int
	require.NoError(t, database.QueryRowContext(t.Context(), query).Scan(&count))

	return count
}

func sqliteBusyError(t *testing.T, path string) error {
	t.Helper()

	first := openInternalSQLite(t, path, 0, false)
	second := openInternalSQLite(t, path, 0, false)
	require.NoError(t, Migrate(t.Context(), first))
	lock, err := first.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	_, err = lock.ExecContext(t.Context(), "UPDATE sessions SET updated_at = updated_at")
	require.NoError(t, err)
	t.Cleanup(func() {
		rollbackErr := lock.Rollback()
		require.True(t, rollbackErr == nil || errors.Is(rollbackErr, sql.ErrTxDone))
	})
	_, err = second.ExecContext(t.Context(), "UPDATE sessions SET updated_at = updated_at")
	require.Error(t, err)

	return fmt.Errorf("create busy error: %w", err)
}

func sqliteBusySnapshotError(t *testing.T, path string) error {
	t.Helper()

	first := openInternalSQLite(t, path, 0, false)
	second := openInternalSQLite(t, path, 0, false)
	require.NoError(t, Migrate(t.Context(), first))
	transaction, err := first.BeginTx(t.Context(), nil)
	require.NoError(t, err)

	var count int
	require.NoError(t, transaction.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM sessions").Scan(&count))
	_, err = second.ExecContext(t.Context(), `INSERT INTO runtime_documents
(namespace, document_key, value_json, updated_at)
VALUES ('snapshot', 'writer', 'value', '2026-01-01T00:00:00Z')`)
	require.NoError(t, err)
	_, err = transaction.ExecContext(t.Context(), "UPDATE sessions SET updated_at = updated_at")
	require.Error(t, err)
	require.NoError(t, transaction.Rollback())

	return fmt.Errorf("create busy snapshot error: %w", err)
}

func sqliteContentionError(t *testing.T, kind string) error {
	t.Helper()
	path := filepath.Join(t.TempDir(), kind+".db")

	switch kind {
	case contentionBusy:
		return sqliteBusyError(t, path)
	case contentionBusySnapshot:
		return sqliteBusySnapshotError(t, path)
	case contentionLocked:
		database, err := sql.Open("sqlite", ":memory:")
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, database.Close()) })
		connection, err := database.Conn(t.Context())
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, connection.Close()) })
		_, err = connection.ExecContext(t.Context(), "CREATE TABLE locked_test (value TEXT)")
		require.NoError(t, err)
		_, err = connection.ExecContext(t.Context(), "INSERT INTO locked_test VALUES ('value')")
		require.NoError(t, err)
		rows, err := connection.QueryContext(t.Context(), "SELECT value FROM locked_test")
		require.NoError(t, err)

		defer func() { require.NoError(t, rows.Close()) }()

		require.True(t, rows.Next())

		_, err = connection.ExecContext(t.Context(), "DROP TABLE locked_test")
		require.Error(t, err)
		require.NoError(t, rows.Err())

		return fmt.Errorf("drop table with active cursor: %w", err)
	default:
		t.Fatalf("unknown contention error %q", kind)

		return nil
	}
}
