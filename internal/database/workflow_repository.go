package database

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/samber/oops"
	"github.com/vingarcia/ksql"
	ksqlite "github.com/vingarcia/ksql/adapters/modernc-ksqlite"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// TaskKindWorkflow identifies durable workflow execution.
const TaskKindWorkflow = "workflow"

// WorkflowRunEntity contains workflow-specific data for a generic task.
type WorkflowRunEntity struct {
	Task          TaskEntity
	Name          string
	Source        string
	SourceHash    string
	SourceVersion string
	ArgumentsJSON string
}

// WorkflowAgentTaskEntity associates an agent task with its workflow-local launch order.
type WorkflowAgentTaskEntity struct {
	CreatedAt       time.Time
	WorkflowTaskID  string
	AgentTaskID     string
	NodeKey         string
	InvocationIndex int
	Sequence        int64
}

// WorkflowAgentTaskDetail combines a workflow link with its complete agent task.
type WorkflowAgentTaskDetail struct {
	AgentTask AgentTaskEntity
	Link      WorkflowAgentTaskEntity
}

// WorkflowRepository persists workflow metadata and composes generic lifecycle operations.
type WorkflowRepository struct {
	sql        ksql.Provider
	tasks      *TaskRepository
	agentTasks *AgentTaskRepository
}

// NewWorkflowRepository creates a workflow repository.
func NewWorkflowRepository(connection *sql.DB) *WorkflowRepository {
	provider, err := ksqlite.NewFromSQLDB(connection)
	if err != nil {
		panic(err)
	}

	return NewWorkflowRepositoryWithProvider(provider)
}

// NewWorkflowRepositoryWithProvider creates a workflow repository with an explicit SQL provider.
func NewWorkflowRepositoryWithProvider(provider ksql.Provider) *WorkflowRepository {
	return &WorkflowRepository{
		sql: provider, tasks: NewTaskRepositoryWithProvider(provider),
		agentTasks: NewAgentTaskRepositoryWithProvider(provider),
	}
}

// Tasks returns the generic task repository used for workflow lifecycle and events.
func (repository *WorkflowRepository) Tasks() *TaskRepository {
	return repository.tasks
}

// AgentTasks returns the agent-task repository sharing this repository's transaction provider.
func (repository *WorkflowRepository) AgentTasks() *AgentTaskRepository {
	return repository.agentTasks
}

// Create persists a queued workflow task, metadata, and initial event atomically.
func (repository *WorkflowRepository) Create(
	ctx context.Context,
	run *WorkflowRunEntity,
) (*WorkflowRunEntity, error) {
	if run == nil {
		return nil, errors.New("database: workflow run is required")
	}

	now := repository.tasks.now().UTC()
	created := *run
	created.Task.ID = newUUIDv7()
	created.Task.Kind = TaskKindWorkflow
	created.Task.State = TaskQueued
	created.Task.CreatedAt = now

	created.Task.UpdatedAt = now
	if created.ArgumentsJSON == "" {
		created.ArgumentsJSON = "{}"
	}

	if err := validateWorkflowRunEntity(&created); err != nil {
		return nil, oops.In("database").Code("validate_workflow_run").Wrapf(err, "validate workflow run")
	}

	if err := repository.sql.Transaction(ctx, func(transaction ksql.Provider) error {
		if err := insertTask(ctx, transaction, &created.Task); err != nil {
			return err
		}

		const statement = `INSERT INTO workflow_runs
(task_id, name, source, source_hash, source_version, arguments_json) VALUES (?, ?, ?, ?, ?, ?)`
		if _, err := transaction.Exec(ctx, statement, created.Task.ID, created.Name, created.Source, created.SourceHash,
			created.SourceVersion, created.ArgumentsJSON); err != nil {
			return oops.In("database").Code("insert_workflow_run").Wrapf(err, "insert workflow run")
		}

		_, err := insertTaskEvent(ctx, transaction, created.Task.ID, 1, "task_queued", "{}", now)

		return err
	}); err != nil {
		return nil, oops.In("database").Code("create_workflow_run").Wrapf(err, "create workflow run")
	}

	return &created, nil
}

// Get loads a workflow run and its generic lifecycle by task ID.
func (repository *WorkflowRepository) Get(
	ctx context.Context,
	taskID string,
) (*WorkflowRunEntity, bool, error) {
	const query = `SELECT ` + workflowRunColumns + ` FROM tasks t
JOIN workflow_runs w ON w.task_id = t.id WHERE t.id = ? AND t.kind = ?`

	return querySQLRow(ctx, repository.sql, workflowRunFromRow, query, "workflow_run", taskID, TaskKindWorkflow)
}

// ListByOwner returns workflow runs belonging to a session, newest first.
func (repository *WorkflowRepository) ListByOwner(
	ctx context.Context,
	ownerSessionID string,
	limit int,
) ([]WorkflowRunEntity, error) {
	if limit <= 0 {
		limit = 100
	}

	const query = `SELECT ` + workflowRunColumns + ` FROM tasks t
JOIN workflow_runs w ON w.task_id = t.id
WHERE t.kind = ? AND t.owner_session_id = ?
ORDER BY t.updated_at DESC, t.id DESC LIMIT ?`

	return querySQLRows(
		ctx, repository.sql, workflowRunFromRow, query, "workflow_run", TaskKindWorkflow, ownerSessionID, limit,
	)
}

// CreateAgentTaskWithChildSession atomically creates a child session, queued agent task, and workflow link.
func (repository *WorkflowRepository) CreateAgentTaskWithChildSession(
	ctx context.Context,
	workflowTaskID string,
	agentTask *AgentTaskEntity,
	childRequest *ChildSessionRequest,
	nodeKey string,
	invocationIndex int,
) (*AgentTaskEntity, error) {
	candidate, child, err := prepareAgentTaskChild(repository.tasks.now(), agentTask, childRequest)
	if err != nil {
		return nil, err
	}

	return repository.createAgentTask(ctx, workflowTaskID, candidate, child, nodeKey, invocationIndex)
}

// CreateAgentTask atomically persists a queued agent task and its workflow link.
func (repository *WorkflowRepository) CreateAgentTask(
	ctx context.Context,
	workflowTaskID string,
	agentTask *AgentTaskEntity,
	nodeKey string,
	invocationIndex int,
) (*AgentTaskEntity, error) {
	return repository.createAgentTask(ctx, workflowTaskID, agentTask, nil, nodeKey, invocationIndex)
}

func (repository *WorkflowRepository) createAgentTask(
	ctx context.Context,
	workflowTaskID string,
	agentTask *AgentTaskEntity,
	child *SessionEntity,
	nodeKey string,
	invocationIndex int,
) (*AgentTaskEntity, error) {
	if err := validateUUIDv7("workflow_agent_task.workflow_task_id", workflowTaskID); err != nil {
		return nil, err
	}

	agents := NewAgentTaskRepositoryWithProvider(repository.sql)

	created, _, err := agents.prepareCreate(agentTask)
	if err != nil {
		return nil, err
	}

	if created.Task.ParentTaskID != workflowTaskID {
		return nil, oops.In("database").Code("workflow_agent_parent_mismatch").
			Errorf("agent task parent must be workflow %q", workflowTaskID)
	}

	request := workflowAgentTaskCreate{
		workflowTaskID:  workflowTaskID,
		agentTask:       created,
		child:           child,
		nodeKey:         strings.TrimSpace(nodeKey),
		invocationIndex: invocationIndex,
	}

	if err := repository.sql.Transaction(ctx, func(transaction ksql.Provider) error {
		return persistWorkflowAgentTask(ctx, transaction, &request)
	}); err != nil {
		return nil, oops.In("database").Code("create_workflow_agent_task").Wrapf(err, "create workflow agent task")
	}

	return created, nil
}

type workflowAgentTaskCreate struct {
	agentTask      *AgentTaskEntity
	child          *SessionEntity
	workflowTaskID string
	nodeKey        string

	invocationIndex int
}

func persistWorkflowAgentTask(
	ctx context.Context,
	transaction ksql.Provider,
	request *workflowAgentTaskCreate,
) error {
	var workflow struct {
		OwnerSessionID string `ksql:"owner_session_id"`
	}

	const workflowQuery = `SELECT t.owner_session_id FROM tasks t
JOIN workflow_runs w ON w.task_id = t.id WHERE t.id = ?`
	if err := transaction.QueryOne(ctx, &workflow, workflowQuery, request.workflowTaskID); err != nil {
		return oops.In("database").Code("load_workflow_agent_parent").Wrapf(err, "load workflow agent parent")
	}

	if workflow.OwnerSessionID != request.agentTask.Task.OwnerSessionID {
		return oops.In("database").Code("workflow_agent_owner_mismatch").
			Errorf("agent task owner differs from workflow owner")
	}

	if request.child != nil {
		if err := insertSession(ctx, transaction, request.child); err != nil {
			return err
		}
	}

	if err := insertAgentTask(ctx, transaction, request.agentTask, request.agentTask.Task.CreatedAt); err != nil {
		return err
	}

	_, err := insertWorkflowAgentTask(
		ctx,
		transaction,
		request.workflowTaskID,
		request.agentTask.Task.ID,
		request.nodeKey,
		request.invocationIndex,
		request.agentTask.Task.CreatedAt,
	)

	return err
}

// LinkAgentTask appends an agent task to a workflow's launch order. Repeating
// the exact link is safe and returns the existing row.
func (repository *WorkflowRepository) LinkAgentTask(
	ctx context.Context,
	workflowTaskID string,
	agentTaskID string,
	nodeKey string,
	invocationIndex int,
) (*WorkflowAgentTaskEntity, error) {
	if err := validateUUIDv7("workflow_agent_task.workflow_task_id", workflowTaskID); err != nil {
		return nil, err
	}

	if err := validateUUIDv7("workflow_agent_task.agent_task_id", agentTaskID); err != nil {
		return nil, err
	}

	var link *WorkflowAgentTaskEntity

	nodeKey = strings.TrimSpace(nodeKey)

	err := repository.sql.Transaction(ctx, func(transaction ksql.Provider) error {
		created, err := insertWorkflowAgentTask(ctx, transaction, workflowTaskID, agentTaskID,
			nodeKey, invocationIndex, repository.tasks.now().UTC())
		if err == nil {
			link = created

			return nil
		}

		if !isWorkflowInvocationUniqueConstraint(err) {
			return err
		}

		existing, found, queryErr := querySQLRow(ctx, transaction, workflowAgentTaskFromRow,
			`SELECT workflow_task_id, agent_task_id, sequence, node_key, invocation_index, created_at
FROM workflow_agent_tasks WHERE workflow_task_id = ? AND node_key = ? AND invocation_index = ?`,
			"workflow_agent_task", workflowTaskID, nodeKey, invocationIndex)
		if queryErr != nil || !found {
			return err
		}

		if existing.AgentTaskID != agentTaskID {
			return oops.In("database").Code("workflow_agent_invocation_conflict").
				Errorf("workflow invocation is already linked to agent task %q", existing.AgentTaskID)
		}

		link = existing

		return nil
	})
	if err != nil {
		return nil, oops.In("database").Code("create_workflow_agent_link").Wrapf(err, "create workflow agent link")
	}

	return link, nil
}

func isWorkflowInvocationUniqueConstraint(err error) bool {
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) || sqliteErr.Code() != sqlite3.SQLITE_CONSTRAINT_UNIQUE {
		return false
	}

	const constraint = "UNIQUE constraint failed: workflow_agent_tasks.workflow_task_id, " +
		"workflow_agent_tasks.node_key, workflow_agent_tasks.invocation_index"

	return strings.Contains(sqliteErr.Error(), constraint)
}

func insertWorkflowAgentTask(
	ctx context.Context,
	transaction ksql.Provider,
	workflowTaskID string,
	agentTaskID string,
	nodeKey string,
	invocationIndex int,
	now time.Time,
) (*WorkflowAgentTaskEntity, error) {
	var row struct {
		Sequence int64 `ksql:"sequence"`
	}

	const statement = `INSERT INTO workflow_agent_tasks
(workflow_task_id, agent_task_id, sequence, node_key, invocation_index, created_at)
SELECT ?, ?, COALESCE(MAX(sequence), 0) + 1, ?, ?, ?
FROM workflow_agent_tasks WHERE workflow_task_id = ?
RETURNING sequence`
	if err := transaction.QueryOne(ctx, &row, statement, workflowTaskID, agentTaskID,
		nodeKey, invocationIndex, formatTime(now), workflowTaskID); err != nil {
		return nil, oops.In("database").Code("link_workflow_agent_task").Wrapf(err, "link workflow agent task")
	}

	return &WorkflowAgentTaskEntity{CreatedAt: now, WorkflowTaskID: workflowTaskID,
		AgentTaskID: agentTaskID, NodeKey: nodeKey, InvocationIndex: invocationIndex, Sequence: row.Sequence}, nil
}

// FindAgentTask returns a linked child by its normalized workflow invocation identity.
func (repository *WorkflowRepository) FindAgentTask(
	ctx context.Context,
	workflowTaskID string,
	nodeKey string,
	invocationIndex int,
) (*WorkflowAgentTaskEntity, bool, error) {
	const query = `SELECT workflow_task_id, agent_task_id, sequence, node_key, invocation_index, created_at
FROM workflow_agent_tasks WHERE workflow_task_id = ? AND node_key = ? AND invocation_index = ?`

	return querySQLRow(ctx, repository.sql, workflowAgentTaskFromRow, query, "workflow_agent_task",
		workflowTaskID, strings.TrimSpace(nodeKey), invocationIndex)
}

// ListAgentTasks returns linked agent tasks in launch order.
func (repository *WorkflowRepository) ListAgentTasks(
	ctx context.Context,
	workflowTaskID string,
) ([]WorkflowAgentTaskEntity, error) {
	const query = `SELECT workflow_task_id, agent_task_id, sequence, node_key, invocation_index, created_at
FROM workflow_agent_tasks WHERE workflow_task_id = ? ORDER BY sequence ASC`

	rows := []workflowAgentTaskRow{}
	if err := repository.sql.Query(ctx, &rows, query, workflowTaskID); err != nil {
		return nil, oops.In("database").Code("list_workflow_agent_tasks").Wrapf(err, "list workflow agent tasks")
	}

	links, err := collectSQLRows(rows, workflowAgentTaskFromRow)
	if err != nil {
		return nil, oops.In("database").Code("scan_workflow_agent_task").Wrapf(err, "scan workflow agent task")
	}

	return links, nil
}

// ListAgentTaskDetails loads linked agent tasks for multiple workflows with two bulk queries.
func (repository *WorkflowRepository) ListAgentTaskDetails(
	ctx context.Context,
	workflowTaskIDs []string,
) ([]WorkflowAgentTaskDetail, error) {
	if len(workflowTaskIDs) == 0 {
		return []WorkflowAgentTaskDetail{}, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(workflowTaskIDs)), ",")
	query := `SELECT workflow_task_id, agent_task_id, sequence, node_key, invocation_index, created_at
FROM workflow_agent_tasks WHERE workflow_task_id IN (` + placeholders + `)
ORDER BY workflow_task_id ASC, sequence ASC`

	arguments := make([]any, len(workflowTaskIDs))
	for index, workflowTaskID := range workflowTaskIDs {
		arguments[index] = workflowTaskID
	}

	rows := []workflowAgentTaskRow{}
	if err := repository.sql.Query(ctx, &rows, query, arguments...); err != nil {
		return nil, oops.In("database").Code("list_workflow_agent_task_details").
			Wrapf(err, "list workflow agent task details")
	}

	links, err := collectSQLRows(rows, workflowAgentTaskFromRow)
	if err != nil {
		return nil, oops.In("database").Code("scan_workflow_agent_task").Wrapf(err, "scan workflow agent task")
	}

	taskIDs := make([]string, len(links))
	for index := range links {
		taskIDs[index] = links[index].AgentTaskID
	}

	tasks, err := repository.agentTasks.ListByIDs(ctx, taskIDs)
	if err != nil {
		return nil, err
	}

	tasksByID := make(map[string]AgentTaskEntity, len(tasks))
	for index := range tasks {
		tasksByID[tasks[index].Task.ID] = tasks[index]
	}

	details := make([]WorkflowAgentTaskDetail, 0, len(links))
	for index := range links {
		task, found := tasksByID[links[index].AgentTaskID]
		if !found {
			continue
		}

		details = append(details, WorkflowAgentTaskDetail{AgentTask: task, Link: links[index]})
	}

	return details, nil
}

const workflowRunColumns = `t.id, t.kind, t.parent_task_id, t.owner_session_id, t.concurrency_key,
t.state, t.result, t.error_code, t.error_message, t.created_at, t.started_at, t.finished_at,
t.updated_at, t.lease_owner, t.lease_expires_at,
w.name, w.source, w.source_hash, w.source_version, w.arguments_json`

type workflowRunRow struct {
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
	Name           string  `ksql:"name"`
	Source         string  `ksql:"source"`
	SourceHash     string  `ksql:"source_hash"`
	SourceVersion  string  `ksql:"source_version"`
	ArgumentsJSON  string  `ksql:"arguments_json"`
}

func workflowRunFromRow(row *workflowRunRow) (*WorkflowRunEntity, error) {
	task, err := taskFromRow(&taskRow{
		ID: row.ID, Kind: row.Kind, ParentTaskID: row.ParentTaskID, OwnerSessionID: row.OwnerSessionID,
		ConcurrencyKey: row.ConcurrencyKey, State: row.State, Result: row.Result, ErrorCode: row.ErrorCode,
		ErrorMessage: row.ErrorMessage, CreatedAt: row.CreatedAt, StartedAt: row.StartedAt,
		FinishedAt: row.FinishedAt, UpdatedAt: row.UpdatedAt, LeaseOwner: row.LeaseOwner,
		LeaseExpiresAt: row.LeaseExpiresAt,
	})
	if err != nil {
		return nil, err
	}

	return &WorkflowRunEntity{Task: *task, Name: row.Name, Source: row.Source, SourceHash: row.SourceHash,
		SourceVersion: row.SourceVersion, ArgumentsJSON: row.ArgumentsJSON}, nil
}

type workflowAgentTaskRow struct {
	CreatedAt       string `ksql:"created_at"`
	WorkflowTaskID  string `ksql:"workflow_task_id"`
	AgentTaskID     string `ksql:"agent_task_id"`
	NodeKey         string `ksql:"node_key"`
	InvocationIndex int    `ksql:"invocation_index"`
	Sequence        int64  `ksql:"sequence"`
}

func workflowAgentTaskFromRow(row *workflowAgentTaskRow) (*WorkflowAgentTaskEntity, error) {
	createdAt, err := parseTime(row.CreatedAt)
	if err != nil {
		return nil, err
	}

	return &WorkflowAgentTaskEntity{CreatedAt: createdAt, WorkflowTaskID: row.WorkflowTaskID,
		AgentTaskID: row.AgentTaskID, NodeKey: row.NodeKey,
		InvocationIndex: row.InvocationIndex, Sequence: row.Sequence}, nil
}
