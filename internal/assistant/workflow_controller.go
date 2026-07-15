package assistant

import (
	"context"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/workflow"
)

// WorkflowController adapts the reusable agent submitter and task controller to
// the workflow runtime without weakening child-session ownership or policy
// snapshot semantics.
type WorkflowController struct {
	submitter *AgentSubmitter
	tasks     AgentTaskController
	sessions  *database.SessionRepository
}

// NewWorkflowController creates the production workflow agent adapter.
func NewWorkflowController(
	submitter *AgentSubmitter,
	tasks AgentTaskController,
	sessions *database.SessionRepository,
) (*WorkflowController, error) {
	if submitter == nil || tasks == nil || sessions == nil {
		return nil, oops.In("assistant").Code("invalid_workflow_controller_dependencies").
			Errorf("agent submitter, task controller, and sessions are required")
	}

	return &WorkflowController{submitter: submitter, tasks: tasks, sessions: sessions}, nil
}

// Submit resolves the owner's working directory and delegates child creation
// and policy capture to AgentSubmitter.
func (controller *WorkflowController) Submit(
	ctx context.Context,
	request *workflow.AgentRequest,
) (*database.AgentTaskEntity, error) {
	if request == nil {
		return nil, oops.In("assistant").Code("nil_workflow_agent_request").Errorf("workflow agent request is nil")
	}

	owner, found, err := controller.sessions.GetSession(ctx, request.OwnerSessionID)
	if err != nil {
		return nil, oops.In("assistant").Code("load_workflow_owner").Wrapf(err, "load workflow owner session")
	}

	if !found {
		return nil, oops.In("assistant").Code("workflow_owner_not_found").
			Errorf("workflow owner session %q was not found", request.OwnerSessionID)
	}

	return controller.submitter.SubmitAgent(ctx, &AgentSubmitRequest{
		ParentTaskID:    request.ParentTaskID,
		OwnerSessionID:  request.OwnerSessionID,
		CWD:             owner.CWD,
		AgentName:       request.Options.AgentName,
		Prompt:          request.Prompt,
		Model:           request.Options.Model,
		Provider:        request.Options.Provider,
		ConcurrencyKey:  request.Options.ConcurrencyKey,
		NodeKey:         request.NodeKey,
		InvocationIndex: request.InvocationIndex,
		Depth:           request.Options.Depth,
	})
}

// Get returns an agent task by ID.
func (controller *WorkflowController) Get(
	ctx context.Context,
	taskID string,
) (*database.AgentTaskEntity, bool, error) {
	task, found, err := controller.tasks.Get(ctx, taskID)
	if err != nil {
		return nil, false, oops.In("assistant").Code("get_workflow_agent_task").Wrapf(err, "get workflow agent task")
	}

	return task, found, nil
}

// List returns agent tasks owned by a workflow session.
func (controller *WorkflowController) List(
	ctx context.Context,
	ownerSessionID string,
	limit int,
) ([]database.AgentTaskEntity, error) {
	tasks, err := controller.tasks.List(ctx, ownerSessionID, limit)
	if err != nil {
		return nil, oops.In("assistant").Code("list_workflow_agent_tasks").Wrapf(err, "list workflow agent tasks")
	}

	return tasks, nil
}

// Await waits for an agent task to reach a terminal state.
func (controller *WorkflowController) Await(
	ctx context.Context,
	taskID string,
) (*database.AgentTaskEntity, error) {
	task, err := controller.tasks.Await(ctx, taskID)
	if err != nil {
		return nil, oops.In("assistant").Code("await_workflow_agent_task").Wrapf(err, "await workflow agent task")
	}

	return task, nil
}

// Cancel requests cancellation of a workflow-owned agent task.
func (controller *WorkflowController) Cancel(
	ctx context.Context,
	ownerSessionID string,
	taskID string,
) (*database.TaskEntity, bool, error) {
	task, found, err := controller.tasks.Cancel(ctx, ownerSessionID, taskID)
	if err != nil {
		return nil, false, oops.In("assistant").Code("cancel_workflow_agent_task").Wrapf(
			err,
			"cancel workflow agent task",
		)
	}

	return task, found, nil
}

var _ workflow.Controller = (*WorkflowController)(nil)
