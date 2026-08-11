package database

import (
	"context"
	"crypto/rand"
	"database/sql"
	"database/sql/driver"
	"errors"
	"log/slog"
	"math/big"
	"time"

	"github.com/samber/oops"
	"github.com/vingarcia/ksql"
	ksqlite "github.com/vingarcia/ksql/adapters/modernc-ksqlite"
	"github.com/vingarcia/ksql/sqldialect"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

const (
	sqlitePrimaryCodeMask     = 0xff
	sqliteTransactionAttempts = 3
	sqliteRetryInitialDelay   = 5 * time.Millisecond
	sqliteRetryMaximumDelay   = 20 * time.Millisecond
	sqliteCleanupTimeout      = time.Second
)

func isSQLiteBusy(err error) bool {
	return sqlitePrimaryCode(err) == sqlite3.SQLITE_BUSY
}

func sqlitePrimaryCode(err error) int {
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) {
		return 0
	}

	return sqliteErr.Code() & sqlitePrimaryCodeMask
}

type transactionMode string

type transactionOutcome string

type transactionPhase string

const (
	transactionModeReadOnly transactionMode = "read_only"
	transactionModeWrite    transactionMode = "write"

	transactionOutcomeSuccess       transactionOutcome = "success"
	transactionOutcomeBusyExhausted transactionOutcome = "busy_exhausted"
	transactionOutcomeLocked        transactionOutcome = "locked"
	transactionOutcomeCanceled      transactionOutcome = "canceled"
	transactionOutcomeDeadline      transactionOutcome = "deadline"
	transactionOutcomeError         transactionOutcome = "error"

	transactionPhaseAttempt   transactionPhase = "attempt"
	transactionPhaseRetryWait transactionPhase = "retry_wait"
)

type transactionDiagnostic struct {
	mode            transactionMode
	outcome         transactionOutcome
	phase           transactionPhase
	attempts        int
	retryDelay      time.Duration
	attemptDuration time.Duration
	primaryCode     int
}

type transactionProvider struct {
	ksql.Provider
	connection *sql.DB

	// These private hooks keep retry policy independently testable without
	// abstracting database/sql transactions or adding a SQL mocking dependency.
	executeAttempt func(context.Context, *sql.TxOptions, func(ksql.Provider) error) error
	diagnostic     func(transactionDiagnostic)
}

type transactionAttemptError struct {
	cause   error
	cleanup error
}

func (err *transactionAttemptError) Error() string {
	return errors.Join(err.cause, err.cleanup).Error()
}

func (err *transactionAttemptError) Unwrap() []error { return []error{err.cause, err.cleanup} }

func (provider *transactionProvider) Transaction(ctx context.Context, callback func(ksql.Provider) error) error {
	return provider.execute(ctx, transactionModeWrite, nil, callback)
}

// readOnlyTransaction is explicit because only proven multi-query snapshots
// should opt out of immediate writer arbitration.
func (provider *transactionProvider) readOnlyTransaction(
	ctx context.Context,
	callback func(ksql.Provider) error,
) error {
	return provider.execute(ctx, transactionModeReadOnly, &sql.TxOptions{ReadOnly: true}, callback)
}

type transactionResult[T any] struct {
	value T
}

func readOnlyTransactionValue[T any](
	ctx context.Context,
	provider ksql.Provider,
	callback func(ksql.Provider) (T, error),
) (*transactionResult[T], error) {
	transactional, ok := provider.(*transactionProvider)
	if !ok {
		// A generic provider cannot express database/sql read-only options, but
		// its transaction boundary still guarantees a consistent snapshot.
		return executeTransactionValue(ctx, provider.Transaction, callback)
	}

	return executeTransactionValue(ctx, transactional.readOnlyTransaction, callback)
}

func transactionValue[T any](
	ctx context.Context,
	provider ksql.Provider,
	callback func(ksql.Provider) (T, error),
) (*transactionResult[T], error) {
	return executeTransactionValue(ctx, provider.Transaction, callback)
}

func executeTransactionValue[T any](
	ctx context.Context,
	execute func(context.Context, func(ksql.Provider) error) error,
	callback func(ksql.Provider) (T, error),
) (*transactionResult[T], error) {
	var committed T

	err := execute(ctx, func(transaction ksql.Provider) error {
		attempt, callbackErr := callback(transaction)
		if callbackErr != nil {
			return callbackErr
		}

		committed = attempt

		return nil
	})
	if err != nil {
		return nil, oops.In("database").Code("transaction_value").Wrapf(
			err, "execute value transaction",
		)
	}

	return &transactionResult[T]{value: committed}, nil
}

func (provider *transactionProvider) execute(
	ctx context.Context,
	mode transactionMode,
	options *sql.TxOptions,
	callback func(ksql.Provider) error,
) error {
	var (
		totalDelay      time.Duration
		attemptDuration time.Duration
	)

	for attempt := 1; attempt <= sqliteTransactionAttempts; attempt++ {
		if contextErr := ctx.Err(); contextErr != nil {
			provider.logTransaction(
				ctx, mode, transactionPhaseAttempt, attempt-1, totalDelay, attemptDuration, contextErr,
			)

			return oops.In("database").Code("transaction_context").Wrapf(contextErr, "transaction context")
		}

		attemptStarted := time.Now()
		err := provider.runAttempt(ctx, options, callback)

		attemptDuration += time.Since(attemptStarted)
		if err == nil {
			provider.logTransaction(ctx, mode, transactionPhaseAttempt, attempt, totalDelay, attemptDuration, nil)

			return nil
		}

		var attemptErr *transactionAttemptError
		if errors.As(err, &attemptErr) && attemptErr.cleanup != nil {
			provider.logTransaction(ctx, mode, transactionPhaseAttempt, attempt, totalDelay, attemptDuration, err)

			return err
		}

		if !isSQLiteBusy(err) {
			provider.logTransaction(ctx, mode, transactionPhaseAttempt, attempt, totalDelay, attemptDuration, err)

			return err
		}

		if attempt == sqliteTransactionAttempts {
			provider.logTransaction(ctx, mode, transactionPhaseAttempt, attempt, totalDelay, attemptDuration, err)

			return oops.In("database").Code("transaction_retry_exhausted").Wrapf(
				err, "transaction exhausted after %d attempts", attempt,
			)
		}

		delay := retryDelay(attempt)

		waitStarted := time.Now()
		if waitErr := waitForRetry(ctx, delay); waitErr != nil {
			totalDelay += time.Since(waitStarted)
			provider.logTransaction(
				ctx, mode, transactionPhaseRetryWait, attempt, totalDelay, attemptDuration, waitErr,
			)

			return waitErr
		}

		totalDelay += time.Since(waitStarted)
	}

	panic("unreachable")
}

func (provider *transactionProvider) runAttempt(
	ctx context.Context,
	options *sql.TxOptions,
	callback func(ksql.Provider) error,
) error {
	if provider.executeAttempt != nil {
		return provider.executeAttempt(ctx, options, callback)
	}

	return provider.attempt(ctx, options, callback)
}

func (provider *transactionProvider) attempt(
	ctx context.Context,
	options *sql.TxOptions,
	callback func(ksql.Provider) error,
) (resultErr error) {
	readOnly := options != nil && options.ReadOnly

	connection, err := provider.connection.Conn(ctx)
	if err != nil {
		return oops.In("database").Code("transaction_connection").Wrapf(err, "acquire transaction connection")
	}

	var transaction *sql.Tx

	defer func() {
		recovered := recover()

		cleanupErr := cleanupTransaction(connection, transaction, readOnly)
		if closeErr := connection.Close(); closeErr != nil {
			cleanupErr = errors.Join(cleanupErr, oops.In("database").Code("transaction_connection_cleanup").Wrapf(
				closeErr, "close transaction connection",
			))
		}

		resultErr = joinTransactionErrors(resultErr, cleanupErr)

		if recovered != nil {
			panic(recovered)
		}
	}()

	transaction, err = connection.BeginTx(ctx, options)
	if err != nil {
		return oops.In("database").Code("begin_transaction").Wrapf(err, "begin transaction")
	}

	if setupErr := enableReadOnly(ctx, transaction, readOnly); setupErr != nil {
		return setupErr
	}

	ksqlTransaction, err := ksql.NewWithAdapter(
		ksqlite.SQLTx{Tx: transaction},
		sqldialect.Sqlite3Dialect{},
	)
	if err != nil {
		return oops.In("database").Code("transaction_provider").Wrapf(
			err, "create transaction provider",
		)
	}

	if callbackErr := callback(ksqlTransaction); callbackErr != nil {
		return callbackErr
	}

	if err := transaction.Commit(); err != nil {
		return oops.In("database").Code("commit_transaction").Wrapf(err, "commit transaction")
	}

	return nil
}

func enableReadOnly(ctx context.Context, transaction *sql.Tx, readOnly bool) error {
	if !readOnly {
		return nil
	}

	if _, err := transaction.ExecContext(ctx, "PRAGMA query_only=ON"); err != nil {
		return oops.In("database").Code("read_only_transaction").Wrapf(
			err, "enable query-only transaction",
		)
	}

	return nil
}

func cleanupTransaction(connection *sql.Conn, transaction *sql.Tx, readOnly bool) error {
	var cleanupErr error

	if transaction != nil {
		if rollbackErr := transaction.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			cleanupErr = oops.In("database").Code("rollback_transaction").Wrapf(
				rollbackErr, "rollback transaction",
			)
		}
	}

	if readOnly {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), sqliteCleanupTimeout)
		defer cancel()

		if _, err := connection.ExecContext(cleanupCtx, "PRAGMA query_only=OFF"); err != nil {
			cleanupErr = errors.Join(cleanupErr, oops.In("database").Code("read_only_transaction_cleanup").Wrapf(
				err, "disable query-only transaction",
			))

			discardConnection(connection)
		}
	}

	return cleanupErr
}

// discardConnection prevents connection-scoped state from leaking back into
// the pool when restoring that state fails.
func discardConnection(connection *sql.Conn) {
	err := connection.Raw(func(any) error { return driver.ErrBadConn })
	if err != nil && !errors.Is(err, driver.ErrBadConn) {
		slog.Debug("discard poisoned sqlite connection", slog.Any("error", err))
	}
}

func joinTransactionErrors(cause, cleanup error) error {
	if cause == nil {
		return cleanup
	}

	if cleanup == nil {
		return cause
	}

	return &transactionAttemptError{cause: cause, cleanup: cleanup}
}

func retryDelay(attempt int) time.Duration {
	delay := min(sqliteRetryInitialDelay<<(attempt-1), sqliteRetryMaximumDelay)

	jitter, err := rand.Int(rand.Reader, big.NewInt(int64(delay)+1))
	if err != nil {
		return delay
	}

	return time.Duration(jitter.Int64())
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return oops.In("database").Code("transaction_retry_context").Wrapf(
			ctx.Err(), "transaction retry context",
		)
	case <-timer.C:
		return nil
	}
}

func (provider *transactionProvider) logTransaction(
	ctx context.Context,
	mode transactionMode,
	phase transactionPhase,
	attempts int,
	retryDelay time.Duration,
	attemptDuration time.Duration,
	err error,
) {
	diagnostic := transactionDiagnostic{
		mode:            mode,
		outcome:         classifyTransactionOutcome(err, attempts),
		phase:           phase,
		attempts:        attempts,
		retryDelay:      retryDelay,
		attemptDuration: attemptDuration,
		primaryCode:     sqlitePrimaryCode(err),
	}
	if provider.diagnostic != nil {
		provider.diagnostic(diagnostic)
	}

	slog.DebugContext(ctx, "sqlite transaction",
		slog.String("mode", string(diagnostic.mode)),
		slog.String("outcome", string(diagnostic.outcome)),
		slog.String("phase", string(diagnostic.phase)),
		slog.Int("attempts", diagnostic.attempts),
		slog.Duration("retry_delay", diagnostic.retryDelay),
		slog.Duration("attempt_duration", diagnostic.attemptDuration),
		slog.Int("sqlite_primary_code", diagnostic.primaryCode),
	)
}

func classifyTransactionOutcome(err error, attempts int) transactionOutcome {
	switch {
	case err == nil:
		return transactionOutcomeSuccess
	case errors.Is(err, context.Canceled):
		return transactionOutcomeCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return transactionOutcomeDeadline
	case sqlitePrimaryCode(err) == sqlite3.SQLITE_LOCKED:
		return transactionOutcomeLocked
	case isSQLiteBusy(err) && attempts >= sqliteTransactionAttempts:
		return transactionOutcomeBusyExhausted
	default:
		return transactionOutcomeError
	}
}
