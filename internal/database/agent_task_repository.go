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

// AgentTaskEntity contains agent-specific data for a generic task.
type AgentTaskEntity struct {
	Task                    TaskEntity
	ChildSessionID          string
	AgentName               string
	Prompt                  string
	Model                   string
	Provider                string
	PolicyJSON              string
	UsageJSON               string
	OutputSchemaJSON        string
	OutputSchemaDigest      string
	OutputValidationSummary string
	OutputAttemptsReserved  int
	OutputAttemptsCompleted int
	Depth                   int
}

// AgentTaskRepository persists the agent-task extension alongside generic tasks.
type AgentTaskRepository struct {
	sql   ksql.Provider
	tasks *TaskRepository
}

// NewAgentTaskRepository creates an agent task repository.
func NewAgentTaskRepository(connection *sql.DB) (*AgentTaskRepository, error) {
	provider, err := newSQLProvider(connection)
	if err != nil {
		return nil, err
	}

	tasks, err := newStandaloneTaskRepository(provider)
	if err != nil {
		return nil, err
	}

	return NewAgentTaskRepositoryWithProvider(provider, tasks)
}

// NewAgentTaskRepositoryWithProvider creates an agent task repository with explicit shared dependencies.
func NewAgentTaskRepositoryWithProvider(
	provider ksql.Provider,
	tasks *TaskRepository,
) (*AgentTaskRepository, error) {
	if isNilProvider(provider) {
		return nil, nilProviderError()
	}

	if tasks == nil {
		return nil, oops.In("database").Code("nil_task_repository").Errorf("task repository is required")
	}

	if !sameSQLProvider(provider, tasks.sql) {
		return nil, oops.In("database").Code("repository_graph_mismatch").Errorf(
			"task repository must share the SQL provider",
		)
	}

	return &AgentTaskRepository{sql: provider, tasks: tasks}, nil
}

// Tasks returns the shared generic task repository.
func (repository *AgentTaskRepository) Tasks() *TaskRepository {
	return repository.tasks
}

// CreateWithChildSession atomically creates a child session and its queued agent task.
func (repository *AgentTaskRepository) CreateWithChildSession(
	ctx context.Context,
	agentTask *AgentTaskEntity,
	childRequest *ChildSessionRequest,
) (*AgentTaskEntity, error) {
	candidate, child, err := prepareAgentTaskChild(repository.tasks.now(), agentTask, childRequest)
	if err != nil {
		return nil, err
	}

	created, now, err := repository.prepareCreate(candidate)
	if err != nil {
		return nil, err
	}

	eventID := newUUIDv7()

	if err := repository.sql.Transaction(ctx, func(transaction ksql.Provider) error {
		if err := insertSession(ctx, transaction, child); err != nil {
			return err
		}

		return insertAgentTask(ctx, transaction, created, now, eventID)
	}); err != nil {
		return nil, oops.In("database").Code("create_agent_task_with_session").
			Wrapf(err, "create agent task with child session")
	}

	return created, nil
}

// Create persists a generic queued task, its initial event, and agent extension atomically.
func (repository *AgentTaskRepository) Create(
	ctx context.Context,
	agentTask *AgentTaskEntity,
) (*AgentTaskEntity, error) {
	created, now, err := repository.prepareCreate(agentTask)
	if err != nil {
		return nil, err
	}

	eventID := newUUIDv7()

	if err := repository.sql.Transaction(ctx, func(transaction ksql.Provider) error {
		return insertAgentTask(ctx, transaction, created, now, eventID)
	}); err != nil {
		return nil, oops.In("database").Code("create_agent_task").Wrapf(err, "create agent task")
	}

	return created, nil
}

func prepareAgentTaskChild(
	now time.Time,
	agentTask *AgentTaskEntity,
	childRequest *ChildSessionRequest,
) (*AgentTaskEntity, *SessionEntity, error) {
	if agentTask == nil {
		return nil, nil, oops.In("database").Code("nil_agent_task").
			Errorf("agent task is required")
	}

	if childRequest == nil {
		return nil, nil, oops.In("database").Code("nil_child_session_request").
			Errorf("child session request is required")
	}

	if agentTask.Task.OwnerSessionID != childRequest.ParentSessionID {
		return nil, nil, errors.New("database: child session parent differs from agent task owner")
	}

	child, err := prepareSession(now, childRequest.CWD, childRequest.Name, childRequest.ParentSessionID)
	if err != nil {
		return nil, nil, err
	}

	candidate := *agentTask
	candidate.ChildSessionID = child.ID

	return &candidate, child, nil
}

func (repository *AgentTaskRepository) prepareCreate(agentTask *AgentTaskEntity) (*AgentTaskEntity, time.Time, error) {
	if agentTask == nil {
		return nil, time.Time{}, oops.In("database").Code("nil_agent_task").
			Errorf("agent task is required")
	}

	now := repository.tasks.now().UTC()
	created := *agentTask
	created.Task.ID = newUUIDv7()
	created.Task.Kind = TaskKindAgent
	created.Task.State = TaskQueued
	created.Task.CreatedAt = now
	created.Task.UpdatedAt = now

	if created.PolicyJSON == "" {
		created.PolicyJSON = "{}"
	}

	if created.UsageJSON == "" {
		created.UsageJSON = "{}"
	}

	if err := validateAgentTaskEntity(&created); err != nil {
		return nil, time.Time{}, oops.In("database").Code("validate_agent_task").Wrapf(err, "validate agent task")
	}

	return &created, now, nil
}

func insertAgentTask(
	ctx context.Context, transaction ksql.Provider, created *AgentTaskEntity, now time.Time, eventID string,
) error {
	if err := insertTask(ctx, transaction, &created.Task); err != nil {
		return err
	}

	const statement = `INSERT INTO agent_tasks (
    task_id, child_session_id, agent_name, prompt, model,
    provider, policy_json, usage_json, output_schema_json, output_schema_digest,
    output_attempts_reserved, output_attempts_completed, output_validation_summary, depth
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	if _, err := transaction.Exec(ctx, statement, created.Task.ID, created.ChildSessionID,
		created.AgentName, created.Prompt, created.Model, created.Provider,
		created.PolicyJSON, created.UsageJSON, nullableString(created.OutputSchemaJSON),
		nullableString(created.OutputSchemaDigest), created.OutputAttemptsReserved,
		created.OutputAttemptsCompleted, created.OutputValidationSummary, created.Depth); err != nil {
		return oops.In("database").Code("insert_agent_task").Wrapf(err, "insert agent task")
	}

	err := insertTaskEvent(ctx, transaction, &taskEventInsert{
		createdAt: now, id: eventID, taskID: created.Task.ID,
		kind: taskQueuedEvent, payload: "{}", sequence: 1})

	return err
}

// Finish atomically records agent usage, terminal task state, and its event.
func (repository *AgentTaskRepository) Finish(
	ctx context.Context,
	finish *TaskFinish,
	usageJSON string,
) (bool, error) {
	if err := validateTaskFinish(finish); err != nil {
		return false, err
	}

	if !json.Valid([]byte(usageJSON)) {
		return false, errors.New("agent_task.usage_json must be valid JSON")
	}

	now := repository.tasks.now().UTC()
	eventID := newUUIDv7()

	changed, err := transactionValue(ctx, repository.sql, func(transaction ksql.Provider) (bool, error) {
		transitioned, err := repository.tasks.finishTransaction(ctx, transaction, finish, now, eventID)
		if err != nil || !transitioned {
			return false, err
		}

		const statement = `UPDATE agent_tasks SET usage_json = ? WHERE task_id = ?`
		if _, err = transaction.Exec(ctx, statement, usageJSON, finish.TaskID); err != nil {
			return false, oops.In("database").Code("update_agent_usage").Wrapf(err, "update agent usage")
		}

		return true, nil
	})
	if err != nil {
		return false, oops.In("database").Code("finish_agent_task").Wrapf(err, "finish agent task")
	}

	return changed.value, nil
}

const (
	scanAgentTaskMessage = "scan agent task"
	agentTaskColumns     = `t.id, t.kind, t.parent_task_id, t.owner_session_id, t.concurrency_key,
       t.state, t.result, t.error_code, t.error_message, t.created_at, t.started_at,
       t.finished_at, t.updated_at, t.lease_owner, t.lease_expires_at,
       a.child_session_id, a.agent_name, a.prompt,
       a.model, a.provider, a.policy_json, a.usage_json,
       a.output_schema_json, a.output_schema_digest, a.output_attempts_reserved,
       a.output_attempts_completed, a.output_validation_summary, a.depth`
)

// Get loads an agent task and its generic lifecycle by task ID.
func (repository *AgentTaskRepository) Get(ctx context.Context, taskID string) (*AgentTaskEntity, bool, error) {
	const query = `SELECT ` + agentTaskColumns + `
FROM tasks t JOIN agent_tasks a ON a.task_id = t.id WHERE t.id = ? AND t.kind = ?`

	var row agentTaskRow
	if err := repository.sql.QueryOne(ctx, &row, query, taskID, TaskKindAgent); err != nil {
		if errors.Is(err, ksql.ErrRecordNotFound) {
			return nil, false, nil
		}

		return nil, false, oops.In("database").Code("get_agent_task").Wrapf(err, "get agent task")
	}

	entity, err := agentTaskFromRow(&row)
	if err != nil {
		return nil, false, oops.In("database").Code("scan_agent_task").Wrapf(err, scanAgentTaskMessage)
	}

	return entity, true, nil
}

// ListByOwner returns complete agent tasks belonging to a session, newest first.
func (repository *AgentTaskRepository) ListByOwner(
	ctx context.Context,
	ownerSessionID string,
	limit int,
) ([]AgentTaskEntity, error) {
	if limit <= 0 {
		limit = 100
	}

	const query = `SELECT ` + agentTaskColumns + `
FROM tasks t JOIN agent_tasks a ON a.task_id = t.id
WHERE t.kind = ? AND t.owner_session_id = ?
ORDER BY t.updated_at DESC, t.id DESC LIMIT ?`

	rows := []agentTaskRow{}
	if err := repository.sql.Query(ctx, &rows, query, TaskKindAgent, ownerSessionID, limit); err != nil {
		return nil, oops.In("database").Code("list_agent_tasks").Wrapf(err, "list agent tasks")
	}

	entities, err := collectSQLRows(rows, agentTaskFromRow)
	if err != nil {
		return nil, oops.In("database").Code("scan_agent_task").Wrapf(err, scanAgentTaskMessage)
	}

	return entities, nil
}

// ListByIDs returns complete agent tasks matching the supplied IDs.
func (repository *AgentTaskRepository) ListByIDs(
	ctx context.Context,
	taskIDs []string,
) ([]AgentTaskEntity, error) {
	if len(taskIDs) == 0 {
		return []AgentTaskEntity{}, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(taskIDs)), ",")
	query := `SELECT ` + agentTaskColumns + `
FROM tasks t JOIN agent_tasks a ON a.task_id = t.id
WHERE t.kind = ? AND t.id IN (` + placeholders + `)`

	arguments := make([]any, 0, len(taskIDs)+1)
	arguments = append(arguments, TaskKindAgent)

	for _, taskID := range taskIDs {
		arguments = append(arguments, taskID)
	}

	rows := []agentTaskRow{}
	if err := repository.sql.Query(ctx, &rows, query, arguments...); err != nil {
		return nil, oops.In("database").Code("list_agent_tasks_by_id").Wrapf(err, "list agent tasks by id")
	}

	entities, err := collectSQLRows(rows, agentTaskFromRow)
	if err != nil {
		return nil, oops.In("database").Code("scan_agent_task").Wrapf(err, scanAgentTaskMessage)
	}

	return entities, nil
}

type agentTaskRow struct {
	ID                      string  `ksql:"id"`
	Kind                    string  `ksql:"kind"`
	ParentTaskID            *string `ksql:"parent_task_id"`
	OwnerSessionID          string  `ksql:"owner_session_id"`
	ConcurrencyKey          string  `ksql:"concurrency_key"`
	State                   string  `ksql:"state"`
	Result                  string  `ksql:"result"`
	ErrorCode               string  `ksql:"error_code"`
	ErrorMessage            string  `ksql:"error_message"`
	CreatedAt               string  `ksql:"created_at"`
	StartedAt               *string `ksql:"started_at"`
	FinishedAt              *string `ksql:"finished_at"`
	UpdatedAt               string  `ksql:"updated_at"`
	LeaseOwner              *string `ksql:"lease_owner"`
	LeaseExpiresAt          *string `ksql:"lease_expires_at"`
	ChildSessionID          string  `ksql:"child_session_id"`
	AgentName               string  `ksql:"agent_name"`
	Prompt                  string  `ksql:"prompt"`
	Model                   string  `ksql:"model"`
	Provider                string  `ksql:"provider"`
	PolicyJSON              string  `ksql:"policy_json"`
	UsageJSON               string  `ksql:"usage_json"`
	OutputSchemaJSON        *string `ksql:"output_schema_json"`
	OutputSchemaDigest      *string `ksql:"output_schema_digest"`
	OutputValidationSummary string  `ksql:"output_validation_summary"`
	OutputAttemptsReserved  int     `ksql:"output_attempts_reserved"`
	OutputAttemptsCompleted int     `ksql:"output_attempts_completed"`
	Depth                   int     `ksql:"depth"`
}

func agentTaskFromRow(row *agentTaskRow) (*AgentTaskEntity, error) {
	task, err := taskFromRow(&taskRow{
		ID: row.ID, Kind: row.Kind, ParentTaskID: row.ParentTaskID,
		OwnerSessionID: row.OwnerSessionID, ConcurrencyKey: row.ConcurrencyKey,
		State: row.State, Result: row.Result, ErrorCode: row.ErrorCode,
		ErrorMessage: row.ErrorMessage, CreatedAt: row.CreatedAt,
		StartedAt: row.StartedAt, FinishedAt: row.FinishedAt, UpdatedAt: row.UpdatedAt,
		LeaseOwner: row.LeaseOwner, LeaseExpiresAt: row.LeaseExpiresAt,
	})
	if err != nil {
		return nil, err
	}

	return &AgentTaskEntity{
		Task: *task, ChildSessionID: row.ChildSessionID, AgentName: row.AgentName,
		Prompt: row.Prompt, Model: row.Model, Provider: row.Provider,
		PolicyJSON: row.PolicyJSON, UsageJSON: row.UsageJSON,
		OutputSchemaJSON:        stringValue(row.OutputSchemaJSON),
		OutputSchemaDigest:      stringValue(row.OutputSchemaDigest),
		OutputAttemptsReserved:  row.OutputAttemptsReserved,
		OutputAttemptsCompleted: row.OutputAttemptsCompleted,
		OutputValidationSummary: row.OutputValidationSummary, Depth: row.Depth,
	}, nil
}

// ReserveOutputAttempt atomically consumes one model-call slot under the active lease.
func (repository *AgentTaskRepository) ReserveOutputAttempt(
	ctx context.Context, taskID, leaseOwner string, maximum int,
) (bool, error) {
	const statement = `UPDATE agent_tasks SET output_attempts_reserved = output_attempts_reserved + 1
WHERE task_id = ? AND output_attempts_reserved < ? AND EXISTS (
 SELECT 1 FROM tasks WHERE id = ? AND state = ? AND lease_owner = ?)`

	result, err := repository.sql.Exec(ctx, statement, taskID, maximum, taskID, TaskRunning, leaseOwner)
	if err != nil {
		return false, oops.In("database").Code("reserve_output_attempt").Wrapf(err, "reserve output attempt")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return false, oops.In("database").Code("reserve_output_attempt_rows").
			Wrapf(err, "read reserved output attempt rows")
	}

	return rows == 1, nil
}

// CheckpointOutputAttempt records one completed response and cumulative usage under the active lease.
func (repository *AgentTaskRepository) CheckpointOutputAttempt(
	ctx context.Context, taskID, leaseOwner, usageJSON, summary string,
) error {
	const statement = `UPDATE agent_tasks SET output_attempts_completed = output_attempts_completed + 1,
 usage_json = ?, output_validation_summary = ? WHERE task_id = ? AND EXISTS (
 SELECT 1 FROM tasks WHERE id = ? AND state = ? AND lease_owner = ?)`

	result, err := repository.sql.Exec(ctx, statement, usageJSON, summary, taskID, taskID, TaskRunning, leaseOwner)
	if err != nil {
		return oops.In("database").Code("checkpoint_output_attempt").Wrapf(err, "checkpoint output attempt")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return oops.In("database").Code("checkpoint_output_attempt_rows").
			Wrapf(err, "read checkpointed output attempt rows")
	}

	if rows != 1 {
		return oops.In("database").Code("output_attempt_lease_lost").Errorf("output attempt lease lost")
	}

	return nil
}
