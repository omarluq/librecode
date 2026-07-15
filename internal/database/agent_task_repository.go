package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/samber/oops"
	"github.com/vingarcia/ksql"
	ksqlite "github.com/vingarcia/ksql/adapters/modernc-ksqlite"
)

// AgentTaskEntity contains agent-specific data for a generic task.
type AgentTaskEntity struct {
	Task           TaskEntity
	ChildSessionID string
	AgentName      string
	Prompt         string
	Model          string
	Provider       string
	PolicyJSON     string
	UsageJSON      string
	Depth          int
}

// AgentTaskRepository persists the agent-task extension alongside generic tasks.
type AgentTaskRepository struct {
	sql   ksql.Provider
	tasks *TaskRepository
}

// NewAgentTaskRepository creates an agent task repository.
func NewAgentTaskRepository(connection *sql.DB) *AgentTaskRepository {
	provider, err := ksqlite.NewFromSQLDB(connection)
	if err != nil {
		panic(err)
	}

	return NewAgentTaskRepositoryWithProvider(provider)
}

// NewAgentTaskRepositoryWithProvider creates an agent task repository with an explicit SQL provider.
func NewAgentTaskRepositoryWithProvider(provider ksql.Provider) *AgentTaskRepository {
	return &AgentTaskRepository{sql: provider, tasks: NewTaskRepositoryWithProvider(provider)}
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

	if err := repository.sql.Transaction(ctx, func(transaction ksql.Provider) error {
		return insertAgentTask(ctx, transaction, created, now)
	}); err != nil {
		return nil, oops.In("database").Code("create_agent_task").Wrapf(err, "create agent task")
	}

	return created, nil
}

func (repository *AgentTaskRepository) prepareCreate(agentTask *AgentTaskEntity) (*AgentTaskEntity, time.Time, error) {
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

func insertAgentTask(ctx context.Context, transaction ksql.Provider, created *AgentTaskEntity, now time.Time) error {
	if err := insertTask(ctx, transaction, &created.Task); err != nil {
		return err
	}

	const statement = `INSERT INTO agent_tasks (
    task_id, child_session_id, agent_name, prompt, model,
    provider, policy_json, usage_json, depth
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	if _, err := transaction.Exec(ctx, statement, created.Task.ID, created.ChildSessionID,
		created.AgentName, created.Prompt, created.Model, created.Provider,
		created.PolicyJSON, created.UsageJSON, created.Depth); err != nil {
		return oops.In("database").Code("insert_agent_task").Wrapf(err, "insert agent task")
	}

	_, err := insertTaskEvent(ctx, transaction, created.Task.ID, 1, "task_queued", "{}", now)

	return err
}

// Finish atomically records agent usage, terminal task state, and its event.
func (repository *AgentTaskRepository) Finish(
	ctx context.Context,
	finish *TaskFinish,
	usageJSON string,
) (bool, error) {
	if !json.Valid([]byte(usageJSON)) {
		return false, errors.New("agent_task.usage_json must be valid JSON")
	}

	changed := false

	err := repository.sql.Transaction(ctx, func(transaction ksql.Provider) error {
		transitioned, err := repository.tasks.finishTransaction(ctx, transaction, finish)
		if err != nil || !transitioned {
			return err
		}

		const statement = `UPDATE agent_tasks SET usage_json = ? WHERE task_id = ?`
		if _, err = transaction.Exec(ctx, statement, usageJSON, finish.TaskID); err != nil {
			return oops.In("database").Code("update_agent_usage").Wrapf(err, "update agent usage")
		}

		changed = true

		return nil
	})
	if err != nil {
		return false, oops.In("database").Code("finish_agent_task").Wrapf(err, "finish agent task")
	}

	return changed, nil
}

const agentTaskColumns = `t.id, t.kind, t.parent_task_id, t.owner_session_id, t.concurrency_key,
       t.state, t.result, t.error_code, t.error_message, t.created_at, t.started_at,
       t.finished_at, t.updated_at, t.lease_owner, t.lease_expires_at,
       a.child_session_id, a.agent_name, a.prompt,
       a.model, a.provider, a.policy_json, a.usage_json, a.depth`

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
		return nil, false, oops.In("database").Code("scan_agent_task").Wrapf(err, "scan agent task")
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
		return nil, oops.In("database").Code("scan_agent_task").Wrapf(err, "scan agent task")
	}

	return entities, nil
}

type agentTaskRow struct {
	ID             string  `ksql:"id"`
	Kind           string  `ksql:"kind"`
	ParentTaskID   *string `ksql:"parent_task_id"`
	OwnerSessionID string  `ksql:"owner_session_id"`
	ConcurrencyKey string  `ksql:"concurrency_key"`
	State          string  `ksql:"state"`
	Result         string  `ksql:"result"`
	ErrorCode      string  `ksql:"error_code"`
	ErrorMessage   string  `ksql:"error_message"`
	CreatedAt      string  `ksql:"created_at"`
	StartedAt      *string `ksql:"started_at"`
	FinishedAt     *string `ksql:"finished_at"`
	UpdatedAt      string  `ksql:"updated_at"`
	LeaseOwner     *string `ksql:"lease_owner"`
	LeaseExpiresAt *string `ksql:"lease_expires_at"`
	ChildSessionID string  `ksql:"child_session_id"`
	AgentName      string  `ksql:"agent_name"`
	Prompt         string  `ksql:"prompt"`
	Model          string  `ksql:"model"`
	Provider       string  `ksql:"provider"`
	PolicyJSON     string  `ksql:"policy_json"`
	UsageJSON      string  `ksql:"usage_json"`
	Depth          int     `ksql:"depth"`
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
		PolicyJSON: row.PolicyJSON, UsageJSON: row.UsageJSON, Depth: row.Depth,
	}, nil
}
