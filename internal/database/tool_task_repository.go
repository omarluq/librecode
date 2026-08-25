package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/samber/oops"
	"github.com/vingarcia/ksql"
)

const (
	toolTaskPersistenceTimeout = 2 * time.Second
	// TaskKindTool identifies a durable background tool invocation.
	TaskKindTool              = "tool"
	maxToolTaskArgumentsBytes = 256 * 1024
	toolTaskOwnerField        = "tool_task.owner_session_id"
	toolTaskIDField           = "tool_task.task_id"
	taskInterruptedEvent      = "task_interrupted"
)

// ToolTaskEntity contains the immutable admitted invocation and canonical outcome.
type ToolTaskEntity struct {
	OutcomeVersion    *int
	OutcomeJSON       *string
	Task              TaskEntity
	WrapperCallID     string
	OwnerSessionID    string
	InvocationID      string
	CWD               string
	ParentCallID      string
	InitiatingEntryID string
	PolicyJSON        string
	DefinitionJSON    string
	ArgumentsJSON     string
	TargetName        string
	SourceSequence    int
	TimeoutSeconds    int
}

type expiredToolTaskRecovery struct {
	expiresBefore time.Time
	now           time.Time
	taskID        string
	leaseOwner    string
	outcome       string
	eventID       string
}

// ToolTaskRepository persists tool task data atomically with generic lifecycle state.
type ToolTaskRepository struct {
	sql   ksql.Provider
	tasks *TaskRepository
}

// NewToolTaskRepository creates a tool task repository backed by connection.
func NewToolTaskRepository(connection *sql.DB) (*ToolTaskRepository, error) {
	provider, err := newSQLProvider(connection)
	if err != nil {
		return nil, err
	}

	tasks, err := newStandaloneTaskRepository(provider)
	if err != nil {
		return nil, err
	}

	return NewToolTaskRepositoryWithProvider(provider, tasks)
}

// NewToolTaskRepositoryWithProvider creates a tool task repository sharing tasks' provider.
func NewToolTaskRepositoryWithProvider(
	provider ksql.Provider,
	tasks *TaskRepository,
) (*ToolTaskRepository, error) {
	if isNilProvider(provider) {
		return nil, nilProviderError()
	}

	if tasks == nil || !sameSQLProvider(provider, tasks.sql) {
		return nil, oops.In("database").Code("repository_graph_mismatch").
			Errorf("tool task repository must share the task SQL provider")
	}

	return &ToolTaskRepository{sql: provider, tasks: tasks}, nil
}

// Tasks returns the shared generic repository.
func (repository *ToolTaskRepository) Tasks() *TaskRepository { return repository.tasks }

// Create atomically accepts a tool task. Duplicate invocation identities return the original task.
func (repository *ToolTaskRepository) Create(ctx context.Context, candidate *ToolTaskEntity) (*ToolTaskEntity, error) {
	if err := validateToolTask(candidate); err != nil {
		return nil, err
	}

	created := *candidate
	if created.PolicyJSON == "" {
		created.PolicyJSON = "{}"
	}

	existing, found, err := repository.GetByInvocation(ctx, created.OwnerSessionID, created.InvocationID)
	if err != nil || found {
		return existing, err
	}

	now := repository.tasks.now().UTC()
	created.Task.ID = newUUIDv7()
	created.Task.Kind = TaskKindTool
	created.Task.State = TaskQueued
	created.Task.OwnerSessionID = created.OwnerSessionID
	created.Task.CreatedAt, created.Task.UpdatedAt = now, now
	created.Task.StartedAt, created.Task.FinishedAt = nil, nil
	created.Task.LeaseOwner, created.Task.LeaseExpiresAt = "", nil
	created.Task.Result, created.Task.ErrorCode, created.Task.ErrorMessage = "", "", ""

	eventID := newUUIDv7()

	err = repository.sql.Transaction(ctx, func(provider ksql.Provider) error {
		insertErr := insertTask(ctx, provider, &created.Task)
		if insertErr != nil {
			return insertErr
		}

		const statement = `INSERT INTO tool_tasks (task_id, target_name, arguments_json, cwd, owner_session_id,
invocation_id, wrapper_call_id, parent_call_id, source_sequence, initiating_entry_id, timeout_seconds, policy_json,
definition_json)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

		_, execErr := provider.Exec(
			ctx, statement, created.Task.ID, created.TargetName, created.ArgumentsJSON, created.CWD,
			created.OwnerSessionID, created.InvocationID, created.WrapperCallID, created.ParentCallID,
			created.SourceSequence, created.InitiatingEntryID, created.TimeoutSeconds, created.PolicyJSON,
			created.DefinitionJSON,
		)
		if execErr != nil {
			return oops.In("database").Code("insert_tool_task").Wrapf(execErr, "insert tool task")
		}

		eventErr := insertTaskEvent(ctx, provider, &taskEventInsert{
			createdAt: now, id: eventID, taskID: created.Task.ID,
			kind: taskQueuedEvent, payload: "{}", sequence: 1})

		return eventErr
	})
	if err != nil {
		// The transaction may have committed before the caller's context was
		// canceled. Resolve that ambiguity by immutable idempotency key using a
		// short context which is not owned by the prompt.
		lookupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), toolTaskPersistenceTimeout)
		defer cancel()

		existing, found, lookupErr := repository.GetByInvocation(
			lookupCtx, created.OwnerSessionID, created.InvocationID,
		)
		if lookupErr == nil && found {
			return existing, nil
		}

		return nil, oops.In("database").Code("create_tool_task").Wrapf(err, "create tool task")
	}

	return &created, nil
}

// Finish atomically commits the canonical structured outcome and owner-fenced terminal lifecycle.
func (repository *ToolTaskRepository) Finish(
	ctx context.Context,
	finish *TaskFinish,
	outcomeJSON string,
) (bool, error) {
	if err := validateTaskFinish(finish); err != nil {
		return false, err
	}

	if !json.Valid([]byte(outcomeJSON)) {
		return false, errors.New("tool_task.outcome_json must be valid JSON")
	}

	now := repository.tasks.now().UTC()
	eventID := newUUIDv7()

	changed, err := transactionValue(ctx, repository.sql, func(tx ksql.Provider) (bool, error) {
		return repository.finishTransaction(ctx, tx, finish, outcomeJSON, now, eventID)
	})
	if err != nil {
		return false, oops.In("database").Code("finish_tool_task").Wrapf(err, "finish tool task")
	}

	return changed.value, nil
}

func (repository *ToolTaskRepository) finishTransaction(
	ctx context.Context,
	provider ksql.Provider,
	finish *TaskFinish,
	outcomeJSON string,
	now time.Time,
	eventID string,
) (bool, error) {
	terminal := *finish

	current, found, err := loadTask(ctx, provider, finish.TaskID)
	if err != nil || !found {
		return false, err
	}

	// Cancellation and completion race on this transaction. Once canceling is
	// durable, it is authoritative for both lifecycle and outcome.
	if current.State == TaskCanceling && finish.TargetState != TaskCanceled {
		terminal.From = []TaskState{TaskCanceling}
		terminal.TargetState = TaskCanceled
		terminal.EventKind = taskCanceledEvent
		terminal.Result = ""
		terminal.ErrorCode = string(TaskCanceled)
		terminal.ErrorMessage = "task canceled"
		terminal.PayloadJSON = `{"error_code":"canceled"}`
		outcomeJSON = `{"error":"task canceled","is_error":true,"result":{"content":[],"details":{}},"truncated":false}`
	}

	transitioned, err := repository.tasks.finishTransaction(ctx, provider, &terminal, now, eventID)
	if err != nil || !transitioned {
		return false, err
	}

	return updateToolOutcome(ctx, provider, finish.TaskID, outcomeJSON)
}

func updateToolOutcome(ctx context.Context, tx ksql.Provider, taskID, outcomeJSON string) (bool, error) {
	const statement = `UPDATE tool_tasks SET outcome_version = 1, outcome_json = ? WHERE task_id = ?`

	result, err := tx.Exec(ctx, statement, outcomeJSON, taskID)
	if err != nil {
		return false, oops.In("database").Code("update_tool_outcome").Wrapf(err, "update tool outcome")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return false, oops.In("database").Code("tool_outcome_rows_affected").
			Wrapf(err, "get updated tool outcome row count")
	}

	if rows != 1 {
		return false, errors.New("database: tool task outcome row missing")
	}

	return true, nil
}

// RecoverExpired atomically interrupts expired tool tasks and stores their canonical outcome.
func (repository *ToolTaskRepository) RecoverExpired(ctx context.Context, expiresBefore time.Time) error {
	const (
		outcome = `{"error":"task worker lease expired","is_error":true,` +
			`"result":{"content":[],"details":{}},"truncated":false}`
		batchSize = 100
	)

	for {
		type recoveryResult struct {
			selected     int
			transitioned int
		}

		now := repository.tasks.now().UTC()

		eventIDs := make([]string, batchSize)
		for index := range eventIDs {
			eventIDs[index] = newUUIDv7()
		}

		result, err := transactionValue(ctx, repository.sql, func(provider ksql.Provider) (recoveryResult, error) {
			selected, transitioned, recoverErr := repository.recoverExpiredBatch(
				ctx, provider, expiresBefore, outcome, batchSize, now, eventIDs,
			)

			return recoveryResult{selected: selected, transitioned: transitioned}, recoverErr
		})
		if err != nil {
			return oops.In("database").Code("recover_expired_tool_tasks").
				Wrapf(err, "recover expired tool tasks")
		}

		if result.value.selected < batchSize || result.value.transitioned == 0 {
			return nil
		}
	}
}

func (repository *ToolTaskRepository) recoverExpiredBatch(
	ctx context.Context,
	provider ksql.Provider,
	expiresBefore time.Time,
	outcome string,
	limit int,
	now time.Time,
	eventIDs []string,
) (selected, transitioned int, err error) {
	var rows []struct {
		ID         string `ksql:"id"`
		LeaseOwner string `ksql:"lease_owner"`
	}

	const query = `SELECT id, COALESCE(lease_owner, '') AS lease_owner FROM tasks
WHERE kind = ? AND state IN (?, ?) AND (lease_expires_at IS NULL OR lease_expires_at <= ?)
ORDER BY created_at, id LIMIT ?`
	if err := provider.Query(ctx, &rows, query, TaskKindTool, TaskRunning, TaskCanceling,
		formatTime(expiresBefore.UTC()), limit); err != nil {
		return 0, 0, oops.In("database").Code("list_expired_tool_tasks").Wrapf(err, "list expired tool tasks")
	}

	for index, row := range rows {
		changed, recoverErr := repository.recoverExpiredTransaction(ctx, provider, &expiredToolTaskRecovery{
			expiresBefore: expiresBefore, now: now, taskID: row.ID,
			leaseOwner: row.LeaseOwner, outcome: outcome, eventID: eventIDs[index],
		})
		if recoverErr != nil {
			return 0, 0, recoverErr
		}

		if changed {
			transitioned++
		}
	}

	return len(rows), transitioned, nil
}

func (repository *ToolTaskRepository) recoverExpiredTransaction(
	ctx context.Context,
	provider ksql.Provider,
	recovery *expiredToolTaskRecovery,
) (bool, error) {
	finish := &TaskFinish{
		TaskID: recovery.taskID, From: []TaskState{TaskRunning, TaskCanceling}, TargetState: TaskInterrupted,
		EventKind: taskInterruptedEvent, Result: "", ErrorCode: "lease_expired",
		ErrorMessage: "task worker lease expired", PayloadJSON: `{"error_code":"lease_expired"}`,
		LeaseOwner: recovery.leaseOwner,
	}

	transitioned, err := repository.tasks.finishTransactionWithExpiry(
		ctx, provider, finish, recovery.expiresBefore, recovery.now, recovery.eventID,
	)
	if err != nil || !transitioned {
		return false, err
	}

	updated, err := updateToolOutcome(ctx, provider, recovery.taskID, recovery.outcome)

	return updated, err
}

const toolTaskColumns = `t.id, t.kind, t.parent_task_id, t.owner_session_id, t.concurrency_key, t.state,
t.result, t.error_code, t.error_message, t.created_at, t.started_at, t.finished_at, t.updated_at,
t.lease_owner, t.lease_expires_at, x.target_name, x.arguments_json, x.cwd, x.owner_session_id AS tool_owner_session_id,
x.invocation_id, x.wrapper_call_id, x.parent_call_id, x.source_sequence, x.initiating_entry_id,
x.timeout_seconds, x.policy_json, x.definition_json, x.outcome_version, x.outcome_json`

type toolTaskRow struct {
	StartedAt          *string `ksql:"started_at"`
	LeaseExpiresAt     *string `ksql:"lease_expires_at"`
	ParentTaskID       *string `ksql:"parent_task_id"`
	LeaseOwner         *string `ksql:"lease_owner"`
	FinishedAt         *string `ksql:"finished_at"`
	OutcomeVersion     *int    `ksql:"outcome_version"`
	OutcomeJSON        *string `ksql:"outcome_json"`
	Kind               string  `ksql:"kind"`
	ToolOwnerSessionID string  `ksql:"tool_owner_session_id"`
	CreatedAt          string  `ksql:"created_at"`
	State              string  `ksql:"state"`
	ConcurrencyKey     string  `ksql:"concurrency_key"`
	UpdatedAt          string  `ksql:"updated_at"`
	OwnerSessionID     string  `ksql:"owner_session_id"`
	ErrorCode          string  `ksql:"error_code"`
	TargetName         string  `ksql:"target_name"`
	ArgumentsJSON      string  `ksql:"arguments_json"`
	CWD                string  `ksql:"cwd"`
	ErrorMessage       string  `ksql:"error_message"`
	InvocationID       string  `ksql:"invocation_id"`
	WrapperCallID      string  `ksql:"wrapper_call_id"`
	ParentCallID       string  `ksql:"parent_call_id"`
	InitiatingEntryID  string  `ksql:"initiating_entry_id"`
	PolicyJSON         string  `ksql:"policy_json"`
	DefinitionJSON     string  `ksql:"definition_json"`
	ID                 string  `ksql:"id"`
	Result             string  `ksql:"result"`
	TimeoutSeconds     int     `ksql:"timeout_seconds"`
	SourceSequence     int     `ksql:"source_sequence"`
}

// Get returns a tool task by ID.
func (repository *ToolTaskRepository) Get(ctx context.Context, taskID string) (*ToolTaskEntity, bool, error) {
	if err := validateUUIDv7(toolTaskIDField, taskID); err != nil {
		return nil, false, err
	}

	return repository.get(ctx, `WHERE t.id = ? AND t.kind = ?`, taskID, TaskKindTool)
}

// GetByInvocation returns the tool task admitted for a session-local invocation ID.
func (repository *ToolTaskRepository) GetByInvocation(
	ctx context.Context,
	ownerSessionID string,
	invocationID string,
) (*ToolTaskEntity, bool, error) {
	if err := validateUUIDv7(toolTaskOwnerField, ownerSessionID); err != nil {
		return nil, false, err
	}

	return repository.get(ctx, `WHERE x.owner_session_id = ? AND x.invocation_id = ?`, ownerSessionID, invocationID)
}

// GetOwned returns a tool task only when it belongs to owner.
func (repository *ToolTaskRepository) GetOwned(
	ctx context.Context,
	owner string,
	taskID string,
) (*ToolTaskEntity, bool, error) {
	if err := validateUUIDv7(toolTaskIDField, taskID); err != nil {
		return nil, false, err
	}

	if err := validateUUIDv7(toolTaskOwnerField, owner); err != nil {
		return nil, false, err
	}

	return repository.get(ctx, `WHERE t.id = ? AND t.owner_session_id = ?`, taskID, owner)
}
func (repository *ToolTaskRepository) get(
	ctx context.Context,
	where string,
	args ...any,
) (*ToolTaskEntity, bool, error) {
	var row toolTaskRow

	query := `SELECT ` + toolTaskColumns + ` FROM tasks t JOIN tool_tasks x ON x.task_id=t.id ` + where
	if err := repository.sql.QueryOne(ctx, &row, query, args...); err != nil {
		if errors.Is(err, ksql.ErrRecordNotFound) {
			return nil, false, nil
		}

		return nil, false, oops.In("database").Code("get_tool_task").Wrapf(err, "get tool task")
	}

	entity, err := toolTaskFromRow(&row)

	return entity, true, err
}

// ListByOwner returns an owner's tool tasks, optionally filtered by state.
func (repository *ToolTaskRepository) ListByOwner(
	ctx context.Context,
	owner string,
	states []TaskState,
	limit int,
) ([]ToolTaskEntity, error) {
	if err := validateUUIDv7(toolTaskOwnerField, owner); err != nil {
		return nil, err
	}

	if limit <= 0 || limit > 100 {
		limit = 100
	}

	args := []any{owner}

	filter := ""
	if len(states) > 0 {
		filter = " AND t.state IN (" + strings.TrimRight(strings.Repeat("?,", len(states)), ",") + ")"
		for _, state := range states {
			args = append(args, state)
		}
	}

	args = append(args, limit)
	rows := []toolTaskRow{}

	query := `SELECT ` + toolTaskColumns +
		` FROM tasks t JOIN tool_tasks x ON x.task_id=t.id WHERE t.owner_session_id=?` + filter +
		` ORDER BY t.updated_at DESC,t.id DESC LIMIT ?`
	if err := repository.sql.Query(ctx, &rows, query, args...); err != nil {
		return nil, oops.In("database").Code("list_tool_tasks").Wrapf(err, "list tool tasks")
	}

	return collectSQLRows(rows, toolTaskFromRow)
}

// Cancel requests owner-scoped cancellation and returns the resulting snapshot.
func (repository *ToolTaskRepository) Cancel(ctx context.Context, owner, taskID string) (*ToolTaskEntity, bool, error) {
	if err := validateUUIDv7(toolTaskIDField, taskID); err != nil {
		return nil, false, err
	}

	if err := validateUUIDv7(toolTaskOwnerField, owner); err != nil {
		return nil, false, err
	}

	now := repository.tasks.now().UTC()
	eventID := newUUIDv7()

	found, err := transactionValue(ctx, repository.sql, func(transaction ksql.Provider) (bool, error) {
		return repository.cancelTransaction(ctx, transaction, owner, taskID, now, eventID)
	})
	if err != nil {
		return nil, false, oops.In("database").Code("cancel_tool_task").Wrapf(err, "cancel tool task")
	}

	if !found.value {
		return nil, false, nil
	}

	return repository.GetOwned(ctx, owner, taskID)
}

func (repository *ToolTaskRepository) cancelTransaction(
	ctx context.Context,
	transaction ksql.Provider,
	owner string,
	taskID string,
	now time.Time,
	eventID string,
) (bool, error) {
	current, found, err := loadTask(ctx, transaction, taskID)
	if err != nil || !found || current.OwnerSessionID != owner || current.Kind != TaskKindTool {
		return false, err
	}

	switch current.State {
	case TaskQueued:
		finish := &TaskFinish{
			TaskID: taskID, From: []TaskState{TaskQueued}, TargetState: TaskCanceled,
			EventKind: taskCanceledEvent, Result: "", ErrorCode: "canceled", ErrorMessage: "task canceled",
			PayloadJSON: `{"error_code":"canceled"}`, LeaseOwner: "",
		}

		const outcome = `{"error":"task canceled","is_error":true,` +
			`"result":{"content":[],"details":{}},"truncated":false}`

		_, err = repository.finishTransaction(ctx, transaction, finish, outcome, now, eventID)
	case TaskRunning:
		_, err = repository.tasks.transition(
			ctx, transaction, taskID, []TaskState{TaskRunning}, TaskCanceling,
			TaskEventDraft{Kind: "task_canceling", PayloadJSON: CancelEventPayload(CancelSourceParent)},
			retryStableTaskOperation{now: now, eventID: eventID},
		)
	case TaskCanceling, TaskSucceeded, TaskFailed, TaskCanceled, TaskInterrupted:
	}

	return true, err
}

func toolTaskFromRow(row *toolTaskRow) (*ToolTaskEntity, error) {
	task, err := taskFromRow(&taskRow{
		StartedAt: row.StartedAt, LeaseExpiresAt: row.LeaseExpiresAt, ParentTaskID: row.ParentTaskID,
		LeaseOwner: row.LeaseOwner, FinishedAt: row.FinishedAt, Result: row.Result, ID: row.ID,
		ErrorCode: row.ErrorCode, ErrorMessage: row.ErrorMessage, CreatedAt: row.CreatedAt, State: row.State,
		ConcurrencyKey: row.ConcurrencyKey, UpdatedAt: row.UpdatedAt, OwnerSessionID: row.OwnerSessionID,
		Kind: row.Kind,
	})
	if err != nil {
		return nil, err
	}

	return &ToolTaskEntity{Task: *task, TargetName: row.TargetName, ArgumentsJSON: row.ArgumentsJSON, CWD: row.CWD,
		OwnerSessionID: row.ToolOwnerSessionID, InvocationID: row.InvocationID, WrapperCallID: row.WrapperCallID,
		ParentCallID: row.ParentCallID, InitiatingEntryID: row.InitiatingEntryID, PolicyJSON: row.PolicyJSON,
		DefinitionJSON: row.DefinitionJSON, OutcomeJSON: row.OutcomeJSON, SourceSequence: row.SourceSequence,
		TimeoutSeconds: row.TimeoutSeconds,
		OutcomeVersion: row.OutcomeVersion}, nil
}

func validateToolTask(task *ToolTaskEntity) error {
	if task == nil {
		return oops.In("database").Code("nil_tool_task").Errorf("tool task is required")
	}

	if missingToolTaskIdentity(task) {
		return errors.New("database: tool task target, cwd, owner, invocation, and wrapper call are required")
	}

	if task.TimeoutSeconds <= 0 {
		return errors.New("database: tool task timeout must be positive")
	}

	if len(task.ArgumentsJSON) > maxToolTaskArgumentsBytes || !jsonObject(task.ArgumentsJSON) {
		return errors.New("database: tool task arguments must be a bounded JSON object")
	}

	policyJSON := task.PolicyJSON
	if policyJSON == "" {
		policyJSON = "{}"
	}

	if !jsonObject(policyJSON) {
		return errors.New("database: tool task policy must be a JSON object")
	}

	if !jsonObject(task.DefinitionJSON) {
		return errors.New("database: tool task definition must be a JSON object")
	}

	return nil
}

func missingToolTaskIdentity(task *ToolTaskEntity) bool {
	return strings.TrimSpace(task.TargetName) == "" ||
		strings.TrimSpace(task.CWD) == "" ||
		strings.TrimSpace(task.OwnerSessionID) == "" ||
		strings.TrimSpace(task.InvocationID) == "" ||
		strings.TrimSpace(task.WrapperCallID) == ""
}

func jsonObject(value string) bool {
	var object map[string]json.RawMessage

	return json.Unmarshal([]byte(value), &object) == nil && object != nil
}
