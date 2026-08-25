package database

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/samber/oops"
	"github.com/vingarcia/ksql"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// TaskKindWorkflow identifies durable workflow execution.
const TaskKindWorkflow = "workflow"

// WorkflowRunEntity contains workflow-specific data for a generic task.
type WorkflowRunEntity struct {
	AdmissionClosedAt *time.Time
	Task              TaskEntity
	Name              string
	Source            string
	SourceHash        string
	SourceVersion     string
	GuestAPIVersion   string
	ArgumentsJSON     string
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
	Link      WorkflowAgentTaskEntity
	AgentTask AgentTaskEntity
}

// WorkflowRepository persists workflow metadata and composes generic lifecycle operations.
type WorkflowRepository struct {
	sql        ksql.Provider
	tasks      *TaskRepository
	agentTasks *AgentTaskRepository
}

// NewWorkflowRepository creates a workflow repository.
func NewWorkflowRepository(connection *sql.DB) (*WorkflowRepository, error) {
	provider, err := newSQLProvider(connection)
	if err != nil {
		return nil, err
	}

	tasks, err := NewTaskRepositoryWithProvider(provider)
	if err != nil {
		return nil, err
	}

	agentTasks, err := NewAgentTaskRepositoryWithProvider(provider, tasks)
	if err != nil {
		return nil, err
	}

	return NewWorkflowRepositoryWithProvider(provider, tasks, agentTasks)
}

// NewWorkflowRepositoryWithProvider creates a workflow repository with explicit shared dependencies.
func NewWorkflowRepositoryWithProvider(
	provider ksql.Provider,
	tasks *TaskRepository,
	agentTasks *AgentTaskRepository,
) (*WorkflowRepository, error) {
	if isNilProvider(provider) {
		return nil, nilProviderError()
	}

	if tasks == nil {
		return nil, oops.In("database").Code("nil_task_repository").Errorf("task repository is required")
	}

	if agentTasks == nil {
		return nil, oops.In("database").Code("nil_agent_task_repository").Errorf("agent task repository is required")
	}

	if agentTasks.Tasks() != tasks || !sameSQLProvider(provider, tasks.sql) ||
		!sameSQLProvider(provider, agentTasks.sql) {
		return nil, oops.In("database").Code("repository_graph_mismatch").Errorf(
			"workflow repositories must share dependencies and the SQL provider",
		)
	}

	return &WorkflowRepository{sql: provider, tasks: tasks, agentTasks: agentTasks}, nil
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
		return nil, oops.In("database").Code("nil_workflow_run").Errorf("workflow run is required")
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

	eventID := newUUIDv7()

	if err := repository.sql.Transaction(ctx, func(transaction ksql.Provider) error {
		if err := insertTask(ctx, transaction, &created.Task); err != nil {
			return err
		}

		const statement = `INSERT INTO workflow_runs
(task_id, name, source, source_hash, source_version, guest_api_version, arguments_json)
VALUES (?, ?, ?, ?, ?, ?, ?)`
		if _, err := transaction.Exec(
			ctx,
			statement,
			created.Task.ID,
			created.Name,
			created.Source,
			created.SourceHash,
			created.SourceVersion,
			created.GuestAPIVersion,
			created.ArgumentsJSON,
		); err != nil {
			return oops.In("database").Code("insert_workflow_run").Wrapf(err, "insert workflow run")
		}

		err := insertTaskEvent(ctx, transaction, &taskEventInsert{
			createdAt: now, id: eventID, taskID: created.Task.ID,
			kind: taskQueuedEvent, payload: "{}", sequence: 1})

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

// ListActiveByOwner returns nonterminal workflows and workflows with active directly linked agents.
func (repository *WorkflowRepository) ListActiveByOwner(
	ctx context.Context,
	ownerSessionID string,
	limit int,
) ([]WorkflowRunEntity, error) {
	if limit <= 0 {
		limit = 100
	}

	const query = `SELECT ` + workflowRunColumns + ` FROM tasks t
JOIN workflow_runs w ON w.task_id = t.id
WHERE t.kind = ? AND t.owner_session_id = ? AND (
	t.state IN (?, ?, ?)
	OR EXISTS (
		SELECT 1 FROM workflow_agent_tasks wat
		JOIN tasks child ON child.id = wat.agent_task_id
		WHERE wat.workflow_task_id = t.id AND child.state IN (?, ?, ?)
	)
)
ORDER BY t.updated_at DESC, t.id DESC LIMIT ?`

	return querySQLRows(
		ctx,
		repository.sql,
		workflowRunFromRow,
		query,
		"active_workflow_run",
		TaskKindWorkflow,
		ownerSessionID,
		TaskQueued,
		TaskRunning,
		TaskCanceling,
		TaskQueued,
		TaskRunning,
		TaskCanceling,
		limit,
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

	created, _, err := repository.agentTasks.prepareCreate(agentTask)
	if err != nil {
		return nil, err
	}

	if created.Task.ParentTaskID != workflowTaskID {
		return nil, oops.In("database").Code("workflow_agent_parent_mismatch").
			Errorf("agent task parent must be workflow %q", workflowTaskID)
	}

	request := workflowAgentTaskCreate{
		eventID:         newUUIDv7(),
		workflowTaskID:  workflowTaskID,
		agentTask:       created,
		child:           child,
		nodeKey:         strings.TrimSpace(nodeKey),
		invocationIndex: invocationIndex,
	}

	result, err := transactionValue(ctx, repository.sql, func(transaction ksql.Provider) (*AgentTaskEntity, error) {
		return persistWorkflowAgentTask(ctx, transaction, &request)
	})
	if err != nil {
		return nil, oops.In("database").Code("create_workflow_agent_task").Wrapf(err, "create workflow agent task")
	}

	return result.value, nil
}

type workflowAgentTaskCreate struct {
	agentTask      *AgentTaskEntity
	child          *SessionEntity
	workflowTaskID string
	eventID        string
	nodeKey        string

	invocationIndex int
}

func persistWorkflowAgentTask(
	ctx context.Context,
	transaction ksql.Provider,
	request *workflowAgentTaskCreate,
) (*AgentTaskEntity, error) {
	if err := validateWorkflowAgentParent(ctx, transaction, request); err != nil {
		return nil, err
	}

	if err := fenceWorkflowAdmission(ctx, transaction, request); err != nil {
		return nil, err
	}

	persisted, found, err := findMatchingWorkflowAgentTask(ctx, transaction, request)
	if err != nil || found {
		return persisted, err
	}

	if request.child != nil {
		if err := insertSession(ctx, transaction, request.child); err != nil {
			return nil, err
		}
	}

	if err := insertAgentTask(ctx, transaction, request.agentTask,
		request.agentTask.Task.CreatedAt, request.eventID); err != nil {
		return nil, err
	}

	if _, err := insertWorkflowAgentTask(ctx, transaction, request.workflowTaskID,
		request.agentTask.Task.ID, request.nodeKey, request.invocationIndex,
		request.agentTask.Task.CreatedAt); err != nil {
		return nil, err
	}

	return request.agentTask, nil
}

func validateWorkflowAgentParent(
	ctx context.Context,
	transaction ksql.Provider,
	request *workflowAgentTaskCreate,
) error {
	var owner struct {
		ID string `ksql:"owner_session_id"`
	}

	if err := transaction.QueryOne(ctx, &owner, `SELECT t.owner_session_id FROM tasks t
JOIN workflow_runs w ON w.task_id = t.id WHERE t.id = ?`, request.workflowTaskID); err != nil {
		return oops.In("database").Code("load_workflow_agent_parent").Wrapf(err, "load workflow agent parent")
	}

	if owner.ID != request.agentTask.Task.OwnerSessionID {
		return oops.In("database").Code("workflow_agent_owner_mismatch").
			Errorf("agent task owner differs from workflow owner")
	}

	return nil
}

func fenceWorkflowAdmission(
	ctx context.Context,
	transaction ksql.Provider,
	request *workflowAgentTaskCreate,
) error {
	const statement = `UPDATE workflow_runs SET admission_closed_at = admission_closed_at
WHERE task_id = ? AND admission_closed_at IS NULL AND EXISTS (
 SELECT 1 FROM tasks WHERE id = ? AND kind = ? AND owner_session_id = ? AND state IN (?, ?))`

	result, err := transaction.Exec(ctx, statement, request.workflowTaskID, request.workflowTaskID,
		TaskKindWorkflow, request.agentTask.Task.OwnerSessionID, TaskQueued, TaskRunning)
	if err != nil {
		return oops.In("database").Code("workflow_admission").Wrapf(err, "fence workflow admission")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return oops.In("database").Code("workflow_admission_rows").Wrapf(err, "read workflow admission")
	}

	if rows != 1 {
		return oops.In("database").Code("run_closed").Errorf("workflow run is closed to new children")
	}

	return nil
}

func findMatchingWorkflowAgentTask(
	ctx context.Context,
	transaction ksql.Provider,
	request *workflowAgentTaskCreate,
) (*AgentTaskEntity, bool, error) {
	existing, found, err := querySQLRow(ctx, transaction, workflowAgentTaskFromRow,
		`SELECT workflow_task_id, agent_task_id, sequence, node_key, invocation_index, created_at
FROM workflow_agent_tasks WHERE workflow_task_id = ? AND node_key = ? AND invocation_index = ?`,
		"workflow_agent_task", request.workflowTaskID, request.nodeKey, request.invocationIndex)
	if err != nil || !found {
		return nil, false, err
	}

	persisted, taskFound, err := querySQLRow(ctx, transaction, agentTaskFromRow,
		`SELECT `+agentTaskColumns+` FROM tasks t JOIN agent_tasks a ON a.task_id = t.id WHERE t.id = ?`,
		"agent_task", existing.AgentTaskID)
	if err != nil {
		return nil, false, err
	}

	if !taskFound || workflowInvocationIdentityOf(persisted) != workflowInvocationIdentityOf(request.agentTask) {
		return nil, false, workflowInvocationConflict("workflow invocation identity does not match persisted child")
	}

	if request.child != nil && persisted.ChildSessionID != request.child.ID {
		return nil, false, workflowInvocationConflict("workflow invocation already has a persisted child session")
	}

	if request.child == nil && persisted.Task.ID != request.agentTask.Task.ID {
		return nil, false, oops.In("database").Code("workflow_agent_invocation_conflict").
			Errorf("workflow invocation is already linked to agent task %q", persisted.Task.ID)
	}

	return persisted, true, nil
}

func workflowInvocationConflict(message string) error {
	return oops.In("database").Code("workflow_agent_invocation_conflict").Errorf("%s", message)
}

type workflowInvocationIdentity struct {
	parentTaskID       string
	ownerSessionID     string
	concurrencyKey     string
	agentName          string
	prompt             string
	model              string
	provider           string
	policyJSON         string
	outputSchemaJSON   string
	outputSchemaDigest string
	depth              int
}

func workflowInvocationIdentityOf(agentTask *AgentTaskEntity) workflowInvocationIdentity {
	return workflowInvocationIdentity{
		parentTaskID:       agentTask.Task.ParentTaskID,
		ownerSessionID:     agentTask.Task.OwnerSessionID,
		concurrencyKey:     agentTask.Task.ConcurrencyKey,
		agentName:          agentTask.AgentName,
		prompt:             agentTask.Prompt,
		model:              agentTask.Model,
		provider:           agentTask.Provider,
		policyJSON:         agentTask.PolicyJSON,
		outputSchemaJSON:   agentTask.OutputSchemaJSON,
		outputSchemaDigest: agentTask.OutputSchemaDigest,
		depth:              agentTask.Depth,
	}
}

// CloseAdmission durably prevents further child admission for an owner-scoped run.
func (repository *WorkflowRepository) CloseAdmission(
	ctx context.Context, ownerSessionID, workflowTaskID string,
) (bool, error) {
	now := repository.tasks.now().UTC()

	result, err := repository.sql.Exec(ctx, `UPDATE workflow_runs
SET admission_closed_at = COALESCE(admission_closed_at, ?)
WHERE task_id = ? AND EXISTS (SELECT 1 FROM tasks WHERE id = ? AND kind = ? AND owner_session_id = ?)`,
		formatTime(now), workflowTaskID, workflowTaskID, TaskKindWorkflow, ownerSessionID)
	if err != nil {
		return false, oops.In("database").Code("close_workflow_admission").Wrapf(err, "close workflow admission")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return false, oops.In("database").Code("workflow_admission_rows").Wrapf(err, "read workflow admission")
	}

	return rows == 1, nil
}

// CancelOwned closes admission and idempotently records cancellation for a run and all linked children.
func (repository *WorkflowRepository) CancelOwned(
	ctx context.Context, ownerSessionID, workflowTaskID, runEventKind, runPayload string,
) (bool, error) {
	now := repository.tasks.now().UTC()

	changed, err := transactionValue(ctx, repository.sql, func(transaction ksql.Provider) (bool, error) {
		return repository.cancelWorkflowTree(
			ctx, transaction, ownerSessionID, workflowTaskID, runEventKind, runPayload, now,
		)
	})
	if err != nil {
		return false, oops.In("database").Code("cancel_workflow_tree").Wrapf(err, "cancel workflow tree")
	}

	return changed.value, nil
}

type workflowChildState struct {
	ID    string `ksql:"id"`
	State string `ksql:"state"`
}

func (repository *WorkflowRepository) cancelWorkflowTree(
	ctx context.Context,
	transaction ksql.Provider,
	ownerSessionID, workflowTaskID, runEventKind, runPayload string,
	now time.Time,
) (bool, error) {
	closed, err := closeWorkflowAdmission(ctx, transaction, ownerSessionID, workflowTaskID, now)
	if err != nil || !closed {
		return false, err
	}

	children, err := listCancelableWorkflowChildren(ctx, transaction, ownerSessionID, workflowTaskID)
	if err != nil {
		return false, err
	}

	for _, child := range children {
		if err := repository.cancelWorkflowChild(ctx, transaction, child, now); err != nil {
			return false, err
		}
	}

	target := TaskCanceled
	if len(children) > 0 {
		target = TaskCanceling
	}

	return repository.tasks.transition(ctx, transaction, workflowTaskID,
		[]TaskState{TaskQueued, TaskRunning}, target,
		TaskEventDraft{Kind: runEventKind, PayloadJSON: runPayload},
		retryStableTaskOperation{now: now, eventID: newUUIDv7()})
}

func closeWorkflowAdmission(
	ctx context.Context,
	transaction ksql.Provider,
	ownerSessionID, workflowTaskID string,
	now time.Time,
) (bool, error) {
	result, err := transaction.Exec(ctx, `UPDATE workflow_runs
SET admission_closed_at = COALESCE(admission_closed_at, ?)
WHERE task_id = ? AND EXISTS (SELECT 1 FROM tasks WHERE id = ? AND kind = ? AND owner_session_id = ?)`,
		formatTime(now), workflowTaskID, workflowTaskID, TaskKindWorkflow, ownerSessionID)
	if err != nil {
		return false, oops.In("database").Code("close_workflow_admission").Wrapf(err, "close workflow admission")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return false, oops.In("database").Code("workflow_admission_rows").Wrapf(err, "read workflow admission")
	}

	return rows == 1, nil
}

func listCancelableWorkflowChildren(
	ctx context.Context,
	transaction ksql.Provider,
	ownerSessionID, workflowTaskID string,
) ([]workflowChildState, error) {
	var children []workflowChildState
	if err := transaction.Query(ctx, &children, `SELECT t.id, t.state FROM tasks t
JOIN workflow_agent_tasks wat ON wat.agent_task_id = t.id
WHERE wat.workflow_task_id = ? AND t.owner_session_id = ? AND t.state IN (?, ?)`,
		workflowTaskID, ownerSessionID, TaskQueued, TaskRunning); err != nil {
		return nil, oops.In("database").Code("list_cancelable_workflow_children").
			Wrapf(err, "list cancelable workflow children")
	}

	return children, nil
}

func (repository *WorkflowRepository) cancelWorkflowChild(
	ctx context.Context,
	transaction ksql.Provider,
	child workflowChildState,
	now time.Time,
) error {
	target := TaskCanceling
	if TaskState(child.State) == TaskQueued {
		target = TaskCanceled
	}

	_, err := repository.tasks.transition(ctx, transaction, child.ID,
		[]TaskState{TaskState(child.State)}, target,
		TaskEventDraft{Kind: taskCanceledEvent, PayloadJSON: CancelEventPayload(CancelSourceWorkflow)},
		retryStableTaskOperation{now: now, eventID: newUUIDv7()})

	return err
}

// FinishOwned closes admission and commits a terminal run outcome only after every admitted child is terminal.
func (repository *WorkflowRepository) FinishOwned(
	ctx context.Context, ownerSessionID string, finish *TaskFinish,
) (bool, error) {
	if err := validateTaskFinish(finish); err != nil {
		return false, err
	}

	now := repository.tasks.now().UTC()

	changed, err := transactionValue(ctx, repository.sql, func(transaction ksql.Provider) (bool, error) {
		closed, closeErr := closeWorkflowAdmission(ctx, transaction, ownerSessionID, finish.TaskID, now)
		if closeErr != nil || !closed {
			return false, closeErr
		}

		var active struct {
			Count int `ksql:"count"`
		}
		if queryErr := transaction.QueryOne(ctx, &active, `SELECT COUNT(*) AS count FROM workflow_agent_tasks wat
JOIN tasks child ON child.id = wat.agent_task_id
WHERE wat.workflow_task_id = ? AND child.owner_session_id = ? AND child.state IN (?, ?, ?)`,
			finish.TaskID, ownerSessionID, TaskQueued, TaskRunning, TaskCanceling); queryErr != nil {
			return false, oops.In("database").Code("count_active_workflow_children").
				Wrapf(queryErr, "count active workflow children")
		}

		if active.Count != 0 {
			return false, nil
		}

		return repository.tasks.finishTransaction(ctx, transaction, finish, now, newUUIDv7())
	})
	if err != nil {
		return false, oops.In("database").Code("finish_workflow_tree").Wrapf(err, "finish workflow tree")
	}

	return changed.value, nil
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

	nodeKey = strings.TrimSpace(nodeKey)
	createdAt := repository.tasks.now().UTC()

	link, err := transactionValue(
		ctx, repository.sql, func(transaction ksql.Provider) (*WorkflowAgentTaskEntity, error) {
			created, err := insertWorkflowAgentTask(ctx, transaction, workflowTaskID, agentTaskID,
				nodeKey, invocationIndex, createdAt)
			if err == nil {
				return created, nil
			}

			if !isWorkflowInvocationUniqueConstraint(err) {
				return nil, err
			}

			existing, found, queryErr := querySQLRow(ctx, transaction, workflowAgentTaskFromRow,
				`SELECT workflow_task_id, agent_task_id, sequence, node_key, invocation_index, created_at
FROM workflow_agent_tasks WHERE workflow_task_id = ? AND node_key = ? AND invocation_index = ?`,
				"workflow_agent_task", workflowTaskID, nodeKey, invocationIndex)
			if queryErr != nil {
				return nil, queryErr
			}

			if !found {
				return nil, err
			}

			if existing.AgentTaskID != agentTaskID {
				return nil, oops.In("database").Code("workflow_agent_invocation_conflict").
					Errorf("workflow invocation is already linked to agent task %q", existing.AgentTaskID)
			}

			return existing, nil
		},
	)
	if err != nil {
		return nil, oops.In("database").Code("create_workflow_agent_link").Wrapf(err, "create workflow agent link")
	}

	return link.value, nil
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
w.name, w.source, w.source_hash, w.source_version, w.guest_api_version, w.arguments_json,
w.admission_closed_at`

type workflowRunRow struct {
	StartedAt         *string `ksql:"started_at"`
	AdmissionClosedAt *string `ksql:"admission_closed_at"`
	ParentTaskID      *string `ksql:"parent_task_id"`
	LeaseExpiresAt    *string `ksql:"lease_expires_at"`
	LeaseOwner        *string `ksql:"lease_owner"`
	FinishedAt        *string `ksql:"finished_at"`
	State             string  `ksql:"state"`
	ConcurrencyKey    string  `ksql:"concurrency_key"`
	ErrorMessage      string  `ksql:"error_message"`
	CreatedAt         string  `ksql:"created_at"`
	Result            string  `ksql:"result"`
	ID                string  `ksql:"id"`
	UpdatedAt         string  `ksql:"updated_at"`
	ErrorCode         string  `ksql:"error_code"`
	OwnerSessionID    string  `ksql:"owner_session_id"`
	Name              string  `ksql:"name"`
	Source            string  `ksql:"source"`
	SourceHash        string  `ksql:"source_hash"`
	SourceVersion     string  `ksql:"source_version"`
	GuestAPIVersion   string  `ksql:"guest_api_version"`
	ArgumentsJSON     string  `ksql:"arguments_json"`
	Kind              string  `ksql:"kind"`
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

	closedAt, err := parseOptionalTime(row.AdmissionClosedAt)
	if err != nil {
		return nil, err
	}

	return &WorkflowRunEntity{
		Task: *task, Name: row.Name, Source: row.Source, SourceHash: row.SourceHash,
		SourceVersion: row.SourceVersion, GuestAPIVersion: row.GuestAPIVersion,
		ArgumentsJSON: row.ArgumentsJSON, AdmissionClosedAt: closedAt,
	}, nil
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
