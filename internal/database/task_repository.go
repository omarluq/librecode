package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/samber/oops"
	"github.com/vingarcia/ksql"
	ksqlite "github.com/vingarcia/ksql/adapters/modernc-ksqlite"
)

// TaskState identifies the durable lifecycle state of a task.
type TaskState string

const (
	eventKindField              = "event.kind"
	readAffectedTaskRowsMessage = "read affected task rows"

	// TaskKindAgent identifies asynchronous subagent work.
	TaskKindAgent = "agent"

	// TaskQueued is accepted work waiting for execution.
	TaskQueued TaskState = "queued"
	// TaskRunning is work currently being executed.
	TaskRunning TaskState = "running"
	// TaskCanceling is running work whose cancellation has been requested.
	TaskCanceling TaskState = "canceling"
	// TaskSucceeded is work that completed successfully.
	TaskSucceeded TaskState = "succeeded"
	// TaskFailed is work that terminated with an error.
	TaskFailed TaskState = "failed"
	// TaskCanceled is work stopped by explicit cancellation.
	TaskCanceled TaskState = "canceled"
	// TaskInterrupted is work abandoned by process interruption.
	TaskInterrupted TaskState = "interrupted"
)

// TaskEntity is the generic durable lifecycle of asynchronous work.
type TaskEntity struct {
	CreatedAt      time.Time
	StartedAt      *time.Time
	FinishedAt     *time.Time
	UpdatedAt      time.Time
	LeaseExpiresAt *time.Time
	ID             string
	Kind           string
	ParentTaskID   string
	OwnerSessionID string
	ConcurrencyKey string
	LeaseOwner     string
	State          TaskState
	Result         string
	ErrorCode      string
	ErrorMessage   string
}

// EventEntity is a durable event envelope independent of its associations.
type EventEntity struct {
	CreatedAt   time.Time
	ID          string
	Kind        string
	PayloadJSON string
}

// TaskEventEntity associates an event with its task-local replay sequence.
type TaskEventEntity struct {
	Event    EventEntity
	TaskID   string
	Sequence int64
}

// TaskFinish describes a conditional terminal task transition.
type TaskFinish struct {
	TaskID       string
	EventKind    string
	Result       string
	ErrorCode    string
	ErrorMessage string
	PayloadJSON  string
	LeaseOwner   string
	TargetState  TaskState
	From         []TaskState
}

// TaskClaim describes a queued task lease acquired by one worker.
type TaskClaim struct {
	LeaseExpiresAt time.Time
	TaskID         string
	LeaseOwner     string
	EventKind      string
}

// TaskRecovery describes the terminal outcome for abandoned leased work.
type TaskRecovery struct {
	ExpiresBefore time.Time
	Kind          string
	EventKind     string
	ErrorCode     string
	ErrorMessage  string
	PayloadJSON   string
	TargetState   TaskState
}

// TaskRepository persists generic task lifecycle state and ordered events.
type TaskRepository struct {
	sql ksql.Provider
	now func() time.Time
}

// NewTaskRepository creates a task repository.
func NewTaskRepository(connection *sql.DB) *TaskRepository {
	provider, err := ksqlite.NewFromSQLDB(connection)
	if err != nil {
		panic(err)
	}

	return NewTaskRepositoryWithProvider(provider)
}

// NewTaskRepositoryWithProvider creates a task repository with an explicit SQL provider.
func NewTaskRepositoryWithProvider(provider ksql.Provider) *TaskRepository {
	return &TaskRepository{sql: provider, now: time.Now}
}

// Create persists a queued task and its initial event atomically.
func (repository *TaskRepository) Create(ctx context.Context, task *TaskEntity) (*TaskEntity, error) {
	now := repository.now().UTC()
	created := *task
	created.ID = newUUIDv7()
	created.State = TaskQueued
	created.CreatedAt = now
	created.UpdatedAt = now

	if err := validateTaskEntity(&created); err != nil {
		return nil, oops.In("database").Code("validate_task").Wrapf(err, "validate task")
	}

	if err := repository.sql.Transaction(ctx, func(transaction ksql.Provider) error {
		if err := insertTask(ctx, transaction, &created); err != nil {
			return err
		}

		_, err := insertTaskEvent(ctx, transaction, created.ID, 1, "task_queued", "{}", now)

		return err
	}); err != nil {
		return nil, oops.In("database").Code("create_task").Wrapf(err, "create task")
	}

	return &created, nil
}

// ClaimQueued atomically moves a queued task to running, assigns its lease, and appends an event.
func (repository *TaskRepository) ClaimQueued(ctx context.Context, claim *TaskClaim) (bool, error) {
	return repository.claim(ctx, claim, TaskQueued)
}

// ClaimInterrupted atomically resumes an interrupted task as running with a new lease.
func (repository *TaskRepository) ClaimInterrupted(ctx context.Context, claim *TaskClaim) (bool, error) {
	return repository.claim(ctx, claim, TaskInterrupted)
}

func (repository *TaskRepository) claim(ctx context.Context, claim *TaskClaim, from TaskState) (bool, error) {
	if claim == nil || strings.TrimSpace(claim.LeaseOwner) == "" || claim.LeaseExpiresAt.IsZero() {
		return false, errors.New("database: task claim requires an owner and expiry")
	}

	if err := validateRequiredText(eventKindField, claim.EventKind); err != nil {
		return false, err
	}

	changed := false

	err := repository.sql.Transaction(ctx, func(transaction ksql.Provider) error {
		now := repository.now().UTC()

		const update = `UPDATE tasks SET state = ?, started_at = COALESCE(started_at, ?), finished_at = NULL,
updated_at = ?, lease_owner = ?, lease_expires_at = ?, result = '', error_code = '', error_message = ''
WHERE id = ? AND state = ?`

		result, err := transaction.Exec(ctx, update, TaskRunning, formatTime(now), formatTime(now),
			claim.LeaseOwner, formatTime(claim.LeaseExpiresAt.UTC()), claim.TaskID, from)
		if err != nil {
			return oops.In("database").Code("claim_task").Wrapf(err, "claim task")
		}

		rows, err := result.RowsAffected()
		if err != nil {
			return oops.In("database").Code("task_rows_affected").Wrapf(err, readAffectedTaskRowsMessage)
		}

		if rows != 1 {
			return nil
		}

		sequence, err := nextTaskEventSequence(ctx, transaction, claim.TaskID)
		if err != nil {
			return err
		}

		_, err = insertTaskEvent(ctx, transaction, claim.TaskID, sequence, claim.EventKind, "{}", now)
		changed = err == nil

		return err
	})
	if err != nil {
		return false, oops.In("database").Code("claim_task").Wrapf(err, "claim task")
	}

	return changed, nil
}

// RenewLease extends a lease only while the same owner still runs or cancels the task.
func (repository *TaskRepository) RenewLease(
	ctx context.Context,
	taskID string,
	leaseOwner string,
	leaseExpiresAt time.Time,
) (bool, error) {
	if strings.TrimSpace(leaseOwner) == "" || leaseExpiresAt.IsZero() {
		return false, errors.New("database: lease renewal requires an owner and expiry")
	}

	const update = `UPDATE tasks SET lease_expires_at = ?, updated_at = ?
WHERE id = ? AND lease_owner = ? AND state IN (?, ?)`

	now := repository.now().UTC()

	result, err := repository.sql.Exec(ctx, update, formatTime(leaseExpiresAt.UTC()), formatTime(now),
		taskID, leaseOwner, TaskRunning, TaskCanceling)
	if err != nil {
		return false, oops.In("database").Code("renew_task_lease").Wrapf(err, "renew task lease")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return false, oops.In("database").Code("task_rows_affected").Wrapf(err, readAffectedTaskRowsMessage)
	}

	return rows == 1, nil
}

// Get loads one task by ID.
func (repository *TaskRepository) Get(ctx context.Context, taskID string) (*TaskEntity, bool, error) {
	task, found, err := loadTask(ctx, repository.sql, taskID)
	if err != nil {
		return nil, false, oops.In("database").Code("get_task").Wrapf(err, "get task")
	}

	return task, found, nil
}

// Transition conditionally changes state and appends its event atomically.
func (repository *TaskRepository) Transition(
	ctx context.Context,
	taskID string,
	from []TaskState,
	targetState TaskState,
	kind string,
) (bool, error) {
	if len(from) == 0 {
		return false, errors.New("database: task transition requires a source state")
	}

	if err := validateRequiredText(eventKindField, kind); err != nil {
		return false, err
	}

	changed := false

	err := repository.sql.Transaction(ctx, func(transaction ksql.Provider) error {
		transitioned, err := repository.transition(ctx, transaction, taskID, from, targetState, kind)
		changed = transitioned

		return err
	})
	if err != nil {
		return false, oops.In("database").Code("transition_task").Wrapf(err, "transition task")
	}

	return changed, nil
}

func (repository *TaskRepository) transition(
	ctx context.Context,
	transaction ksql.Provider,
	taskID string,
	from []TaskState,
	targetState TaskState,
	kind string,
) (bool, error) {
	current, found, err := loadTask(ctx, transaction, taskID)
	if err != nil || !found || !slices.Contains(from, current.State) {
		return false, err
	}

	now := repository.now().UTC()
	startedAt, finishedAt := transitionTimes(current, targetState, now)

	const update = `
UPDATE tasks
SET state = ?, started_at = ?, finished_at = ?, updated_at = ?,
    lease_owner = CASE WHEN ? THEN NULL ELSE lease_owner END,
    lease_expires_at = CASE WHEN ? THEN NULL ELSE lease_expires_at END
WHERE id = ? AND state = ? AND (? = FALSE OR lease_owner IS NULL)`

	terminal := isTerminalTaskState(targetState)

	result, err := transaction.Exec(
		ctx, update, targetState, nullableTime(startedAt), nullableTime(finishedAt),
		formatTime(now), terminal, terminal, taskID, current.State, terminal,
	)
	if err != nil {
		return false, oops.In("database").Code("update_task_state").Wrapf(err, "update task state")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return false, oops.In("database").Code("task_rows_affected").Wrapf(err, readAffectedTaskRowsMessage)
	}

	if rows != 1 {
		return false, nil
	}

	sequence, err := nextTaskEventSequence(ctx, transaction, taskID)
	if err != nil {
		return false, err
	}

	if _, err := insertTaskEvent(ctx, transaction, taskID, sequence, kind, "{}", now); err != nil {
		return false, err
	}

	return true, nil
}

func transitionTimes(
	task *TaskEntity,
	targetState TaskState,
	now time.Time,
) (startedAt, finishedAt *time.Time) {
	startedAt, finishedAt = task.StartedAt, task.FinishedAt
	if targetState == TaskRunning && startedAt == nil {
		startedAt = &now
	}

	if isTerminalTaskState(targetState) {
		finishedAt = &now
	}

	return startedAt, finishedAt
}

// Finish conditionally records a terminal outcome and appends its event atomically.
func (repository *TaskRepository) Finish(ctx context.Context, finish *TaskFinish) (bool, error) {
	if finish == nil || len(finish.From) == 0 || !isTerminalTaskState(finish.TargetState) {
		return false, errors.New("database: task finish requires source states and a terminal target")
	}

	if err := validateEvent(finish.EventKind, finish.PayloadJSON); err != nil {
		return false, oops.In("database").Code("validate_event").Wrapf(err, "validate terminal event")
	}

	changed := false

	err := repository.sql.Transaction(ctx, func(transaction ksql.Provider) error {
		var transactionErr error

		changed, transactionErr = repository.finishTransaction(ctx, transaction, finish)

		return transactionErr
	})
	if err != nil {
		return false, oops.In("database").Code("finish_task").Wrapf(err, "finish task")
	}

	return changed, nil
}

func (repository *TaskRepository) finishTransaction(
	ctx context.Context,
	transaction ksql.Provider,
	finish *TaskFinish,
) (bool, error) {
	current, found, err := loadTask(ctx, transaction, finish.TaskID)
	if err != nil || !found || !slices.Contains(finish.From, current.State) {
		return false, err
	}

	now := repository.now().UTC()
	startedAt, finishedAt := transitionTimes(current, finish.TargetState, now)

	const update = `
UPDATE tasks
SET state = ?, result = ?, error_code = ?, error_message = ?,
    started_at = ?, finished_at = ?, updated_at = ?, lease_owner = NULL, lease_expires_at = NULL
WHERE id = ? AND state = ?
  AND ((? = '' AND lease_owner IS NULL) OR lease_owner = ?)`

	updateResult, err := transaction.Exec(
		ctx, update, finish.TargetState, finish.Result, finish.ErrorCode, finish.ErrorMessage,
		nullableTime(startedAt), nullableTime(finishedAt), formatTime(now), finish.TaskID, current.State,
		finish.LeaseOwner, finish.LeaseOwner,
	)
	if err != nil {
		return false, oops.In("database").Code("finish_task").Wrapf(err, "finish task")
	}

	rows, err := updateResult.RowsAffected()
	if err != nil {
		return false, oops.In("database").Code("task_rows_affected").Wrapf(err, readAffectedTaskRowsMessage)
	}

	if rows != 1 {
		return false, nil
	}

	sequence, err := nextTaskEventSequence(ctx, transaction, finish.TaskID)
	if err != nil {
		return false, err
	}

	if _, err := insertTaskEvent(
		ctx, transaction, finish.TaskID, sequence, finish.EventKind, finish.PayloadJSON, now,
	); err != nil {
		return false, err
	}

	return true, nil
}

// RecoverExpired atomically finishes running or canceling tasks whose leases are absent or expired.
// It returns the IDs that were recovered.
func (repository *TaskRepository) RecoverExpired(ctx context.Context, recovery *TaskRecovery) ([]string, error) {
	if recovery == nil || !isTerminalTaskState(recovery.TargetState) {
		return nil, errors.New("database: task recovery requires a terminal target")
	}

	if err := validateEvent(recovery.EventKind, recovery.PayloadJSON); err != nil {
		return nil, oops.In("database").Code("validate_event").Wrapf(err, "validate recovery event")
	}

	recovered := []string{}

	err := repository.sql.Transaction(ctx, func(transaction ksql.Provider) error {
		var rows []struct {
			ID string `ksql:"id"`
		}

		const query = `SELECT id FROM tasks WHERE kind = ? AND state IN (?, ?)
AND (lease_expires_at IS NULL OR lease_expires_at <= ?) ORDER BY created_at, id`
		if err := transaction.Query(ctx, &rows, query, recovery.Kind, TaskRunning, TaskCanceling,
			formatTime(recovery.ExpiresBefore.UTC())); err != nil {
			return oops.In("database").Code("query_expired_tasks").Wrapf(err, "query expired tasks")
		}

		now := repository.now().UTC()

		const update = `UPDATE tasks SET state = ?, error_code = ?, error_message = ?, finished_at = ?,
updated_at = ?, lease_owner = NULL, lease_expires_at = NULL WHERE id = ? AND state IN (?, ?)
AND (lease_expires_at IS NULL OR lease_expires_at <= ?)`
		for _, row := range rows {
			wasRecovered, err := recoverExpiredTask(ctx, transaction, update, row.ID, recovery, now)
			if err != nil {
				return err
			}

			if wasRecovered {
				recovered = append(recovered, row.ID)
			}
		}

		return nil
	})
	if err != nil {
		return nil, oops.In("database").Code("recover_tasks").Wrapf(err, "recover expired tasks")
	}

	return recovered, nil
}

func recoverExpiredTask(
	ctx context.Context,
	transaction ksql.Provider,
	update string,
	taskID string,
	recovery *TaskRecovery,
	now time.Time,
) (bool, error) {
	result, err := transaction.Exec(ctx, update, recovery.TargetState, recovery.ErrorCode,
		recovery.ErrorMessage, formatTime(now), formatTime(now), taskID, TaskRunning,
		TaskCanceling, formatTime(recovery.ExpiresBefore.UTC()))
	if err != nil {
		return false, oops.In("database").Code("recover_task").Wrapf(err, "recover expired task")
	}

	count, err := result.RowsAffected()
	if err != nil {
		return false, oops.In("database").Code("task_rows_affected").Wrapf(err, readAffectedTaskRowsMessage)
	}

	if count != 1 {
		return false, nil
	}

	sequence, err := nextTaskEventSequence(ctx, transaction, taskID)
	if err != nil {
		return false, err
	}

	if _, err = insertTaskEvent(
		ctx, transaction, taskID, sequence, recovery.EventKind, recovery.PayloadJSON, now,
	); err != nil {
		return false, err
	}

	return true, nil
}

// ListByOwner returns tasks of a kind belonging to a session, newest first.
func (repository *TaskRepository) ListByOwner(
	ctx context.Context,
	kind string,
	ownerSessionID string,
	limit int,
) ([]TaskEntity, error) {
	if limit <= 0 {
		limit = 100
	}

	const query = `SELECT ` + taskColumns + ` FROM tasks
WHERE kind = ? AND owner_session_id = ? ORDER BY updated_at DESC, id DESC LIMIT ?`

	rows := []taskRow{}
	if err := repository.sql.Query(ctx, &rows, query, kind, ownerSessionID, limit); err != nil {
		return nil, oops.In("database").Code("list_tasks").Wrapf(err, "list tasks")
	}

	tasks, err := collectSQLRows(rows, taskFromRow)
	if err != nil {
		return nil, oops.In("database").Code("scan_task").Wrapf(err, "scan task")
	}

	return tasks, nil
}

// ListByStates returns tasks of a kind in any requested state, oldest first for recovery and dispatch.
// A non-positive limit returns every matching task.
func (repository *TaskRepository) ListByStates(
	ctx context.Context,
	kind string,
	states []TaskState,
	limit int,
) ([]TaskEntity, error) {
	if len(states) == 0 {
		return nil, nil
	}

	placeholders := strings.TrimRight(strings.Repeat("?,", len(states)), ",")

	args := []any{kind}

	for _, state := range states {
		args = append(args, state)
	}

	query := `SELECT ` + taskColumns + ` FROM tasks WHERE kind = ? AND state IN (` + placeholders + `)
ORDER BY created_at ASC, id ASC`
	if limit > 0 {
		query += " LIMIT ?"

		args = append(args, limit)
	}

	rows := []taskRow{}
	if err := repository.sql.Query(ctx, &rows, query, args...); err != nil {
		return nil, oops.In("database").Code("list_tasks_by_state").Wrapf(err, "list tasks by state")
	}

	tasks, err := collectSQLRows(rows, taskFromRow)
	if err != nil {
		return nil, oops.In("database").Code("scan_task").Wrapf(err, "scan task")
	}

	return tasks, nil
}

// AppendEvent appends a durable event at the next task-local sequence.
func (repository *TaskRepository) AppendEvent(
	ctx context.Context,
	taskID string,
	kind string,
	payloadJSON string,
) (*TaskEventEntity, error) {
	if err := validateEvent(kind, payloadJSON); err != nil {
		return nil, oops.In("database").Code("validate_event").Wrapf(err, "validate event")
	}

	var created *TaskEventEntity

	err := repository.sql.Transaction(ctx, func(transaction ksql.Provider) error {
		sequence, err := nextTaskEventSequence(ctx, transaction, taskID)
		if err != nil {
			return err
		}

		now := repository.now().UTC()

		eventID, err := insertTaskEvent(ctx, transaction, taskID, sequence, kind, payloadJSON, now)
		if err != nil {
			return err
		}

		created = &TaskEventEntity{
			TaskID:   taskID,
			Sequence: sequence,
			Event: EventEntity{
				ID: eventID, Kind: kind, PayloadJSON: payloadJSON, CreatedAt: now,
			},
		}

		return nil
	})
	if err != nil {
		return nil, oops.In("database").Code("append_task_event").Wrapf(err, "append task event")
	}

	return created, nil
}

// LatestEvent returns the newest event associated with a task.
func (repository *TaskRepository) LatestEvent(
	ctx context.Context,
	taskID string,
) (*TaskEventEntity, bool, error) {
	const query = `
SELECT te.task_id, te.sequence, e.id, e.kind, e.payload_json, e.created_at
FROM task_events te JOIN events e ON e.id = te.event_id
WHERE te.task_id = ? ORDER BY te.sequence DESC LIMIT 1`

	var row taskEventRow
	if err := repository.sql.QueryOne(ctx, &row, query, taskID); err != nil {
		if errors.Is(err, ksql.ErrRecordNotFound) {
			return nil, false, nil
		}

		return nil, false, oops.In("database").Code("get_latest_task_event").Wrapf(err, "get latest task event")
	}

	event, err := taskEventFromRow(&row)
	if err != nil {
		return nil, false, oops.In("database").Code("scan_task_event").Wrapf(err, "scan task event")
	}

	return event, true, nil
}

// ListEvents returns task events after sequence in ascending replay order.
func (repository *TaskRepository) ListEvents(
	ctx context.Context,
	taskID string,
	after int64,
	limit int,
) ([]TaskEventEntity, error) {
	if limit <= 0 {
		limit = 100
	}

	const query = `
SELECT te.task_id, te.sequence, e.id, e.kind, e.payload_json, e.created_at
FROM task_events te JOIN events e ON e.id = te.event_id
WHERE te.task_id = ? AND te.sequence > ?
ORDER BY te.sequence ASC LIMIT ?`

	rows := []taskEventRow{}
	if err := repository.sql.Query(ctx, &rows, query, taskID, after, limit); err != nil {
		return nil, oops.In("database").Code("list_task_events").Wrapf(err, "list task events")
	}

	events, err := collectSQLRows(rows, taskEventFromRow)
	if err != nil {
		return nil, oops.In("database").Code("scan_task_event").Wrapf(err, "scan task event")
	}

	return events, nil
}

const taskColumns = `
id, kind, parent_task_id, owner_session_id, concurrency_key, state, result,
error_code, error_message, created_at, started_at, finished_at, updated_at,
lease_owner, lease_expires_at`

type taskRow struct {
	StartedAt      *string `ksql:"started_at"`
	LeaseExpiresAt *string `ksql:"lease_expires_at"`
	ParentTaskID   *string `ksql:"parent_task_id"`
	LeaseOwner     *string `ksql:"lease_owner"`
	FinishedAt     *string `ksql:"finished_at"`
	Result         string  `ksql:"result"`
	ID             string  `ksql:"id"`
	ErrorCode      string  `ksql:"error_code"`
	ErrorMessage   string  `ksql:"error_message"`
	CreatedAt      string  `ksql:"created_at"`
	State          string  `ksql:"state"`
	ConcurrencyKey string  `ksql:"concurrency_key"`
	UpdatedAt      string  `ksql:"updated_at"`
	OwnerSessionID string  `ksql:"owner_session_id"`
	Kind           string  `ksql:"kind"`
}
type taskEventRow struct {
	TaskID      string `ksql:"task_id"`
	ID          string `ksql:"id"`
	Kind        string `ksql:"kind"`
	PayloadJSON string `ksql:"payload_json"`
	CreatedAt   string `ksql:"created_at"`
	Sequence    int64  `ksql:"sequence"`
}

func insertTask(ctx context.Context, provider ksql.Provider, task *TaskEntity) error {
	const statement = `INSERT INTO tasks (` + taskColumns + `)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := provider.Exec(
		ctx, statement, task.ID, task.Kind, nullableString(task.ParentTaskID),
		task.OwnerSessionID, task.ConcurrencyKey, task.State, task.Result,
		task.ErrorCode, task.ErrorMessage, formatTime(task.CreatedAt),
		nullableTime(task.StartedAt), nullableTime(task.FinishedAt), formatTime(task.UpdatedAt),
		nullableString(task.LeaseOwner), nullableTime(task.LeaseExpiresAt),
	)
	if err != nil {
		return oops.In("database").Code("insert_task").Wrapf(err, "insert task")
	}

	return nil
}

func loadTask(ctx context.Context, provider ksql.Provider, taskID string) (*TaskEntity, bool, error) {
	var row taskRow
	if err := provider.QueryOne(ctx, &row, `SELECT `+taskColumns+` FROM tasks WHERE id = ?`, taskID); err != nil {
		if errors.Is(err, ksql.ErrRecordNotFound) {
			return nil, false, nil
		}

		return nil, false, oops.In("database").Code("query_task").Wrapf(err, "query task")
	}

	task, err := taskFromRow(&row)

	return task, true, err
}

func taskFromRow(row *taskRow) (*TaskEntity, error) {
	createdAt, err := parseTime(row.CreatedAt)
	if err != nil {
		return nil, err
	}

	updatedAt, err := parseTime(row.UpdatedAt)
	if err != nil {
		return nil, err
	}

	startedAt, err := parseOptionalTime(row.StartedAt)
	if err != nil {
		return nil, err
	}

	finishedAt, err := parseOptionalTime(row.FinishedAt)
	if err != nil {
		return nil, err
	}

	leaseExpiresAt, err := parseOptionalTime(row.LeaseExpiresAt)
	if err != nil {
		return nil, err
	}

	return &TaskEntity{
		CreatedAt: createdAt, StartedAt: startedAt, FinishedAt: finishedAt, UpdatedAt: updatedAt,
		LeaseExpiresAt: leaseExpiresAt, ID: row.ID, Kind: row.Kind,
		ParentTaskID: stringValue(row.ParentTaskID), OwnerSessionID: row.OwnerSessionID,
		ConcurrencyKey: row.ConcurrencyKey, LeaseOwner: stringValue(row.LeaseOwner),
		State: TaskState(row.State), Result: row.Result, ErrorCode: row.ErrorCode,
		ErrorMessage: row.ErrorMessage,
	}, nil
}

func taskEventFromRow(row *taskEventRow) (*TaskEventEntity, error) {
	createdAt, err := parseTime(row.CreatedAt)
	if err != nil {
		return nil, err
	}

	return &TaskEventEntity{
		TaskID:   row.TaskID,
		Sequence: row.Sequence,
		Event: EventEntity{
			CreatedAt: createdAt, ID: row.ID, Kind: row.Kind, PayloadJSON: row.PayloadJSON,
		},
	}, nil
}

func nextTaskEventSequence(ctx context.Context, provider ksql.Provider, taskID string) (int64, error) {
	var row struct {
		Sequence int64 `ksql:"sequence"`
	}

	const query = `SELECT COALESCE(MAX(sequence), 0) + 1 AS sequence FROM task_events WHERE task_id = ?`
	if err := provider.QueryOne(ctx, &row, query, taskID); err != nil {
		return 0, oops.In("database").Code("next_task_event_sequence").Wrapf(err, "get task event sequence")
	}

	return row.Sequence, nil
}

func insertTaskEvent(
	ctx context.Context,
	provider ksql.Provider,
	taskID string,
	sequence int64,
	kind string,
	payload string,
	createdAt time.Time,
) (string, error) {
	if err := validateEvent(kind, payload); err != nil {
		return "", err
	}

	eventID := newUUIDv7()

	const insertEvent = `INSERT INTO events (id, kind, payload_json, created_at) VALUES (?, ?, ?, ?)`
	if _, err := provider.Exec(ctx, insertEvent, eventID, kind, payload, formatTime(createdAt)); err != nil {
		return "", oops.In("database").Code("insert_event").Wrapf(err, "insert event")
	}

	const associate = `INSERT INTO task_events (task_id, event_id, sequence) VALUES (?, ?, ?)`
	if _, err := provider.Exec(ctx, associate, taskID, eventID, sequence); err != nil {
		return "", oops.In("database").Code("associate_task_event").Wrapf(err, "associate task event")
	}

	return eventID, nil
}

func parseOptionalTime(value *string) (parsed *time.Time, err error) {
	if value != nil {
		var timestamp time.Time

		timestamp, err = parseTime(*value)
		parsed = &timestamp
	}

	return parsed, err
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}

	return value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}

	return formatTime(*value)
}

func validateEvent(kind, payload string) error {
	if err := validateRequiredText(eventKindField, kind); err != nil {
		return err
	}

	if !json.Valid([]byte(payload)) {
		return errors.New("event.payload_json must be valid JSON")
	}

	return nil
}

func isTerminalTaskState(state TaskState) bool {
	return state == TaskSucceeded || state == TaskFailed || state == TaskCanceled || state == TaskInterrupted
}
