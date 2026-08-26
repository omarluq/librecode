package database_test

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/testutil"
)

const (
	soakHelperEnvironment = "LIBRECODE_SQLITE_SOAK_HELPER"
	soakProcessTimeout    = 10 * time.Second
	soakExpiredMessage    = "expired"
	soakRestartErrorCode  = "process_restart"
)

type soakResult struct {
	Status string `json:"status"`
	Count  int    `json:"count,omitempty"`
}

type soakScanResult struct {
	err  error
	line string
}

type soakProcess struct {
	command   *exec.Cmd
	stdin     io.WriteCloser
	lines     chan soakScanResult
	scanDone  chan struct{}
	cancel    context.CancelFunc
	stdinErr  error
	waitErr   error
	stderr    strings.Builder
	stdinOnce sync.Once
	waitOnce  sync.Once
}

type soakFixture struct {
	connection        *sql.DB
	repository        *database.SessionRepository
	tasks             *database.TaskRepository
	environment       map[string]string
	sessionID         string
	compactionSession string
	parentID          string
	taskID            string
}

// TestSQLiteContentionMultiProcessSoak exercises SQLite file locking through
// genuinely independent processes. It has isolated storage and deliberately
// creates sustained contention among its helpers.
func TestSQLiteContentionMultiProcessSoak(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping multi-process SQLite contention soak in short mode")
	}

	if runtime.GOOS != "linux" {
		t.Skip("multi-process SQLite contention soak requires Linux process isolation")
	}

	fixture := newSoakFixture(t)
	runConcurrentAppendSoak(t, fixture.environment)
	runCompactionAndClaimSoak(t, fixture.environment)
	runLifecycleSoak(t, fixture.environment)
	assertSoakState(t, &fixture)
}

func newSoakFixture(t *testing.T) soakFixture {
	t.Helper()

	ctx := t.Context()
	databasePath := filepath.Join(t.TempDir(), "contention-soak.db")
	connection := openTestSQLite(t, databasePath, 2*time.Second)
	repository := testutil.SessionRepository(t, connection)
	tasks := testutil.TaskRepository(t, connection)
	require.NoError(t, database.Migrate(ctx, connection))

	session, err := repository.CreateSession(ctx, t.TempDir(), "multi-process soak", "")
	require.NoError(t, err)
	compactionSession, err := repository.CreateSession(ctx, t.TempDir(), "compaction race", "")
	require.NoError(t, err)
	parent, err := repository.AppendMessage(ctx, compactionSession.ID, nil, soakMessage("compaction parent"))
	require.NoError(t, err)
	task, err := tasks.Create(ctx, &database.TaskEntity{
		CreatedAt: time.Time{}, StartedAt: nil, FinishedAt: nil, UpdatedAt: time.Time{}, LeaseExpiresAt: nil,
		ID: "", Kind: database.TaskKindAgent, ParentTaskID: "", OwnerSessionID: session.ID,
		ConcurrencyKey: "", LeaseOwner: "", State: "", Result: "", ErrorCode: "", ErrorMessage: "",
	})
	require.NoError(t, err)

	return soakFixture{
		connection: connection, repository: repository, tasks: tasks,
		environment: map[string]string{
			"LIBRECODE_SQLITE_SOAK_DB": databasePath, "LIBRECODE_SQLITE_SOAK_SESSION": session.ID,
			"LIBRECODE_SQLITE_SOAK_COMPACT_SESSION": compactionSession.ID,
			"LIBRECODE_SQLITE_SOAK_PARENT":          parent.ID, "LIBRECODE_SQLITE_SOAK_TASK": task.ID,
		},
		sessionID: session.ID, compactionSession: compactionSession.ID, parentID: parent.ID, taskID: task.ID,
	}
}

func runConcurrentAppendSoak(t *testing.T, environment map[string]string) {
	t.Helper()

	appenders := make([]*soakProcess, 3)
	for index := range appenders {
		appenders[index] = startSoakProcess(t, environment, "append", fmt.Sprintf("writer-%d", index))
	}

	results := releaseAndCollectResults(t, appenders)
	for _, result := range results {
		assert.Equal(t, soakResult{Status: "ok", Count: 4}, result)
	}
}

func runCompactionAndClaimSoak(t *testing.T, environment map[string]string) {
	t.Helper()
	compactors := startSoakProcesses(t, environment, "compact", "summary-a", "summary-b")
	assert.ElementsMatch(t, []string{"ok", "stale"}, releaseAndCollect(t, compactors))
	claimers := startSoakProcesses(t, environment, "claim", "worker-a", "worker-b")
	assert.ElementsMatch(t, []string{"claimed", "fenced"}, releaseAndCollect(t, claimers))
}

func startSoakProcesses(t *testing.T, environment map[string]string, mode string, arguments ...string) []*soakProcess {
	t.Helper()

	processes := make([]*soakProcess, len(arguments))
	for index, argument := range arguments {
		processes[index] = startSoakProcess(t, environment, mode, argument)
	}

	return processes
}

func releaseAndCollect(t *testing.T, processes []*soakProcess) []string {
	t.Helper()

	results := releaseAndCollectResults(t, processes)
	statuses := make([]string, len(results))

	for index := range results {
		statuses[index] = results[index].Status
	}

	return statuses
}

// releaseAndCollectResults uses a second barrier so every helper has consumed
// its initial release before any helper starts its database operation.
func releaseAndCollectResults(t *testing.T, processes []*soakProcess) []soakResult {
	t.Helper()

	for _, process := range processes {
		process.startOperation(t)
	}

	for _, process := range processes {
		process.awaitOperationStart(t)
	}

	for _, process := range processes {
		process.releaseOperation(t)
	}

	results := make([]soakResult, len(processes))
	for index, process := range processes {
		results[index] = process.result(t)
	}

	return results
}

func runLifecycleSoak(t *testing.T, environment map[string]string) {
	t.Helper()
	recovery := startSoakProcess(t, environment, "recover", "")
	assert.Equal(t, soakResult{Status: "ok", Count: 1}, releaseAndCollectResults(t, []*soakProcess{recovery})[0])

	locker := startSoakProcess(t, environment, "lock", "")
	canceled := startSoakProcess(t, environment, "cancel", "")
	assert.Equal(t, "canceled", releaseAndCollectResults(t, []*soakProcess{canceled})[0].Status)
	locker.releaseOperation(t)
	assert.Equal(t, "unlocked", locker.result(t).Status)

	shutdown := startSoakProcess(t, environment, "shutdown", "")
	assert.Equal(t, "closed", releaseAndCollectResults(t, []*soakProcess{shutdown})[0].Status)
}

func assertSoakState(t *testing.T, fixture *soakFixture) {
	t.Helper()
	ctx := t.Context()
	messages, err := fixture.repository.Messages(ctx, fixture.sessionID)
	require.NoError(t, err)
	require.Len(t, messages, 13)

	contents := make(map[string]struct{}, len(messages))
	for index := range messages {
		contents[messages[index].Content] = struct{}{}
	}

	assert.Len(t, contents, len(messages), "logical appends must not be duplicated")

	children, err := fixture.repository.Children(ctx, fixture.compactionSession, &fixture.parentID)
	require.NoError(t, err)
	require.Len(t, children, 1)
	assert.Equal(t, database.EntryTypeCompaction, children[0].Type)

	loadedTask, found, err := fixture.tasks.Get(ctx, fixture.taskID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, database.TaskInterrupted, loadedTask.State)
	assert.Empty(t, loadedTask.LeaseOwner)

	events, err := fixture.tasks.ListEvents(ctx, fixture.taskID, 0, 100)
	require.NoError(t, err)
	assert.Len(t, events, 3)

	var canceledSessions int
	require.NoError(t, fixture.connection.QueryRowContext(
		ctx, `SELECT COUNT(*) FROM sessions WHERE name = ?`, "must cancel",
	).Scan(&canceledSessions))
	assert.Zero(t, canceledSessions, "canceled writes must not persist")

	var integrity string
	require.NoError(t, fixture.connection.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity))
	assert.Equal(t, "ok", integrity)
}

// TestSQLiteContentionSoakHelper is invoked only as a subprocess by the soak.
func TestSQLiteContentionSoakHelper(t *testing.T) {
	t.Parallel()

	mode := os.Getenv(soakHelperEnvironment)
	if mode == "" {
		t.Skip("subprocess helper")
	}

	if err := runSQLiteSoakHelper(mode, os.Getenv("LIBRECODE_SQLITE_SOAK_ARG")); err != nil {
		t.Fatal(err)
	}
}

func runSQLiteSoakHelper(mode, argument string) (resultErr error) {
	ctx := context.Background()

	connection, err := sql.Open("sqlite", database.SQLiteDSN(
		os.Getenv("LIBRECODE_SQLITE_SOAK_DB"), database.SQLiteOptions{BusyTimeout: 2 * time.Second},
	))
	if err != nil {
		return fmt.Errorf("open soak database: %w", err)
	}
	defer func() { resultErr = errors.Join(resultErr, closeSoakConnection(connection)) }()

	stdin := bufio.NewReader(os.Stdin)
	if mode == "lock" {
		return runSoakLock(ctx, connection, stdin)
	}

	if barrierErr := awaitSoakOperationBarrier(stdin); barrierErr != nil {
		return barrierErr
	}

	repositories, err := database.NewRepositories(connection)
	if err != nil {
		return fmt.Errorf("create repositories: %w", err)
	}

	result, err := executeSoakMode(ctx, mode, argument, repositories.Sessions, repositories.Tasks, connection)
	if err != nil {
		return err
	}

	return writeSoakResult(result)
}

func awaitSoakOperationBarrier(stdin *bufio.Reader) error {
	if err := signalSoakReady(); err != nil {
		return err
	}

	if err := awaitSoakRelease(stdin); err != nil {
		return err
	}

	if err := signalSoakOperationStart(); err != nil {
		return err
	}

	return awaitSoakRelease(stdin)
}

func executeSoakMode(
	ctx context.Context,
	mode string,
	argument string,
	sessions *database.SessionRepository,
	tasks *database.TaskRepository,
	connection *sql.DB,
) (soakResult, error) {
	switch mode {
	case "append":
		return runAppendSoak(ctx, argument, sessions)
	case "compact":
		return runCompactSoak(ctx, argument, sessions)
	case "claim":
		return runClaimSoak(ctx, argument, tasks)
	case "recover":
		return runRecoverSoak(ctx, tasks)
	case "cancel":
		return runCancelSoak(ctx, sessions)
	case "shutdown":
		return runShutdownSoak(ctx, sessions, connection)
	default:
		return soakResult{Status: "", Count: 0}, fmt.Errorf("unknown soak helper mode %q", mode)
	}
}

func runAppendSoak(ctx context.Context, argument string, sessions *database.SessionRepository) (soakResult, error) {
	for index := range 4 {
		_, err := sessions.AppendMessage(ctx, os.Getenv("LIBRECODE_SQLITE_SOAK_SESSION"), nil,
			soakMessage(fmt.Sprintf("%s-%d", argument, index)))
		if err != nil {
			return soakResult{Status: "", Count: 0}, fmt.Errorf("append soak message: %w", err)
		}
	}

	return soakResult{Status: "ok", Count: 4}, nil
}

func runCompactSoak(ctx context.Context, argument string, sessions *database.SessionRepository) (soakResult, error) {
	parentID := os.Getenv("LIBRECODE_SQLITE_SOAK_PARENT")

	_, err := sessions.AppendCompaction(ctx, &database.AppendCompactionInput{
		ParentID: &parentID, Details: nil, SessionID: os.Getenv("LIBRECODE_SQLITE_SOAK_COMPACT_SESSION"),
		Summary: argument, FirstKeptEntryID: parentID, OperationID: argument, TokensBefore: 100, FromHook: false,
	})
	switch {
	case errors.Is(err, database.ErrStaleCompactionParent):
		return soakResult{Status: "stale", Count: 0}, nil
	case err != nil:
		return soakResult{Status: "", Count: 0}, fmt.Errorf("append compaction: %w", err)
	default:
		return soakResult{Status: "ok", Count: 0}, nil
	}
}

func runClaimSoak(ctx context.Context, argument string, tasks *database.TaskRepository) (soakResult, error) {
	claimed, err := tasks.ClaimQueued(ctx, &database.TaskClaim{
		TaskID: os.Getenv("LIBRECODE_SQLITE_SOAK_TASK"), LeaseOwner: argument,
		EventKind: "soak_started", LeaseExpiresAt: time.Unix(1, 0),
	})
	if err != nil {
		return soakResult{Status: "", Count: 0}, fmt.Errorf("claim queued task: %w", err)
	}

	if claimed {
		return soakResult{Status: "claimed", Count: 0}, nil
	}

	return soakResult{Status: "fenced", Count: 0}, nil
}

func runRecoverSoak(ctx context.Context, tasks *database.TaskRepository) (soakResult, error) {
	recovered, err := tasks.RecoverExpired(ctx, &database.TaskRecovery{
		Kind: database.TaskKindAgent, TargetState: database.TaskInterrupted, EventKind: "soak_interrupted",
		ErrorCode: soakRestartErrorCode, ErrorMessage: soakExpiredMessage,
		PayloadJSON: `{}`, ExpiresBefore: time.Unix(2, 0),
	})
	if err != nil {
		return soakResult{Status: "", Count: 0}, fmt.Errorf("recover expired task: %w", err)
	}

	return soakResult{Status: "ok", Count: len(recovered)}, nil
}

func runCancelSoak(ctx context.Context, sessions *database.SessionRepository) (soakResult, error) {
	cancelContext, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	_, err := sessions.CreateSession(cancelContext, os.TempDir(), "must cancel", "")
	if !isSoakCancellation(err) {
		return soakResult{Status: "", Count: 0}, fmt.Errorf("expected context cancellation, got %w", err)
	}

	return soakResult{Status: "canceled", Count: 0}, nil
}

func isSoakCancellation(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}

	var sqliteErr *sqlite.Error

	return errors.As(err, &sqliteErr) && sqliteErr.Code()&sqliteTestPrimaryCodeMask == sqlite3.SQLITE_INTERRUPT
}

func runShutdownSoak(
	ctx context.Context,
	sessions *database.SessionRepository,
	connection *sql.DB,
) (soakResult, error) {
	_, err := sessions.AppendMessage(
		ctx, os.Getenv("LIBRECODE_SQLITE_SOAK_SESSION"), nil, soakMessage("shutdown-append"),
	)
	if err != nil {
		return soakResult{Status: "", Count: 0}, fmt.Errorf("append shutdown message: %w", err)
	}

	if err = connection.Close(); err != nil {
		return soakResult{Status: "", Count: 0}, fmt.Errorf("close shutdown database: %w", err)
	}

	return soakResult{Status: "closed", Count: 0}, nil
}

func runSoakLock(ctx context.Context, connection *sql.DB, stdin *bufio.Reader) (resultErr error) {
	transaction, err := connection.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin soak lock transaction: %w", err)
	}
	defer func() {
		rollbackErr := transaction.Rollback()
		if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			resultErr = errors.Join(resultErr, fmt.Errorf("rollback soak lock transaction: %w", rollbackErr))
		}
	}()

	if _, err := transaction.ExecContext(ctx, `UPDATE sessions SET updated_at = updated_at`); err != nil {
		return fmt.Errorf("acquire soak write lock: %w", err)
	}

	if readyErr := signalSoakReady(); readyErr != nil {
		return readyErr
	}

	if releaseErr := awaitSoakRelease(stdin); releaseErr != nil {
		return releaseErr
	}

	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit soak lock transaction: %w", err)
	}

	return writeSoakResult(soakResult{Status: "unlocked", Count: 0})
}

func closeSoakConnection(connection *sql.DB) error {
	if err := connection.Close(); err != nil {
		return fmt.Errorf("close soak database: %w", err)
	}

	return nil
}

func soakMessage(content string) *database.MessageEntity {
	return &database.MessageEntity{
		Timestamp: time.Unix(1, 0).UTC(), Role: database.RoleUser, Content: content,
		Provider: "", Model: "", Parts: nil,
	}
}

func signalSoakReady() error {
	if _, err := fmt.Fprintln(os.Stdout, "READY"); err != nil {
		return fmt.Errorf("signal soak readiness: %w", err)
	}

	return nil
}

func signalSoakOperationStart() error {
	if _, err := fmt.Fprintln(os.Stdout, "STARTED"); err != nil {
		return fmt.Errorf("signal soak operation start: %w", err)
	}

	return nil
}

func awaitSoakRelease(stdin *bufio.Reader) error {
	if _, err := stdin.ReadString('\n'); err != nil {
		return fmt.Errorf("await soak release: %w", err)
	}

	return nil
}

func writeSoakResult(result soakResult) error {
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		return fmt.Errorf("write soak result: %w", err)
	}

	return nil
}

func startSoakProcess(t *testing.T, environment map[string]string, mode, argument string) *soakProcess {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), soakProcessTimeout)
	// Start this exact Linux test binary from the fixed procfs path. Each helper
	// is therefore a genuinely separate process without accepting an executable.
	command := exec.CommandContext(ctx, "/proc/self/exe", "-test.run=^TestSQLiteContentionSoakHelper$", "-test.v=false")

	command.Env = append(os.Environ(), soakHelperEnvironment+"="+mode, "LIBRECODE_SQLITE_SOAK_ARG="+argument)
	for key, value := range environment {
		command.Env = append(command.Env, key+"="+value)
	}

	stdin, err := command.StdinPipe()
	if err != nil {
		cancel()
		require.NoError(t, err)
	}

	stdout, err := command.StdoutPipe()
	if err != nil {
		cancel()
		require.NoError(t, errors.Join(err, stdin.Close()))
	}

	process := &soakProcess{
		command: command, stdin: stdin, lines: make(chan soakScanResult, 8),
		scanDone: make(chan struct{}), cancel: cancel,
		stdinErr: nil, waitErr: nil, stderr: strings.Builder{}, stdinOnce: sync.Once{}, waitOnce: sync.Once{},
	}

	command.Stderr = &process.stderr

	if err = command.Start(); err != nil {
		cancel()
		process.closeStdin()
		require.NoError(t, errors.Join(err, process.stdinErr))
	}

	go process.scanOutput(bufio.NewScanner(stdout))

	t.Cleanup(func() { process.stop(true) })

	line, scanErr := process.scanLine()
	if scanErr != nil || line != "READY" {
		process.stop(true)
		require.NoError(t, scanErr, "helper stderr: %s", process.stderr.String())
		require.Equal(t, "READY", line, "helper stderr: %s", process.stderr.String())
	}

	return process
}

func (process *soakProcess) startOperation(t *testing.T) {
	t.Helper()

	_, err := io.WriteString(process.stdin, "GO\n")
	require.NoError(t, err)
}

func (process *soakProcess) awaitOperationStart(t *testing.T) {
	t.Helper()

	line, err := process.scanLine()
	if err != nil || line != "STARTED" {
		process.stop(true)
		require.NoError(t, err, "helper stderr: %s", process.stderr.String())
		require.Equal(t, "STARTED", line, "helper stderr: %s", process.stderr.String())
	}
}

func (process *soakProcess) releaseOperation(t *testing.T) {
	t.Helper()

	_, err := io.WriteString(process.stdin, "GO\n")
	require.NoError(t, err)
	process.closeStdin()
	require.NoError(t, process.stdinErr)
}

func (process *soakProcess) result(t *testing.T) soakResult {
	t.Helper()

	line, scanErr := process.scanLine()
	if scanErr != nil {
		process.stop(true)
		require.NoError(t, scanErr, "helper stderr: %s", process.stderr.String())
	}

	var result soakResult
	if err := json.Unmarshal([]byte(line), &result); err != nil {
		process.stop(true)
		require.NoError(t, err, "helper output: %q; stderr: %s", line, process.stderr.String())
	}

	process.stop(false)
	require.NoError(t, process.waitErr, "helper stderr: %s", process.stderr.String())

	return result
}

func (process *soakProcess) stop(cancelFirst bool) {
	if cancelFirst {
		process.cancel()
	}

	process.closeStdin()

	process.waitOnce.Do(func() {
		<-process.scanDone
		process.waitErr = process.command.Wait()
	})
	process.cancel()
}

func (process *soakProcess) closeStdin() {
	process.stdinOnce.Do(func() {
		process.stdinErr = process.stdin.Close()
	})
}

func (process *soakProcess) scanOutput(scanner *bufio.Scanner) {
	defer close(process.scanDone)

	for scanner.Scan() {
		process.lines <- soakScanResult{err: nil, line: scanner.Text()}
	}

	err := scanner.Err()
	if err == nil {
		err = io.EOF
	}

	process.lines <- soakScanResult{err: err, line: ""}

	close(process.lines)
}

func (process *soakProcess) scanLine() (string, error) {
	select {
	case scanned := <-process.lines:
		return scanned.line, scanned.err
	case <-time.After(soakProcessTimeout):
		process.stop(true)

		return "", errors.New("timed out waiting for soak helper")
	}
}
