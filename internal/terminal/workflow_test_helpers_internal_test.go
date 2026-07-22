package terminal

import (
	"context"

	"github.com/omarluq/librecode/internal/database"
)

const (
	workflowTestSessionID      = "session-1"
	workflowTestForeignSession = "another-session"
	workflowTestWorking        = "working"
	workflowTestCompacting     = "compacting"
)

type workflowInspectorStub struct {
	listErr       error
	getErr        error
	eventsErr     error
	agentTasksErr error
	detailsErr    error
	getRun        *database.WorkflowRunEntity
	runs          []database.WorkflowRunEntity
	events        []database.TaskEventEntity
	children      []database.WorkflowAgentTaskEntity
	details       []database.WorkflowAgentTaskDetail
	found         bool
}

func (stub *workflowInspectorStub) Get(
	context.Context,
	string,
) (*database.WorkflowRunEntity, bool, error) {
	return stub.getRun, stub.found, stub.getErr
}

func (stub *workflowInspectorStub) List(
	context.Context,
	string,
	int,
) ([]database.WorkflowRunEntity, error) {
	return stub.runs, stub.listErr
}

func (stub *workflowInspectorStub) Events(
	context.Context, string, int64, int,
) ([]database.TaskEventEntity, error) {
	return stub.events, stub.eventsErr
}

func (stub *workflowInspectorStub) AgentTasks(
	context.Context, string,
) ([]database.WorkflowAgentTaskEntity, error) {
	return stub.children, stub.agentTasksErr
}

func (stub *workflowInspectorStub) AgentTask(
	context.Context, string,
) (*database.AgentTaskEntity, bool, error) {
	return nil, false, nil
}

func (stub *workflowInspectorStub) AgentTaskDetails(
	context.Context, []string,
) ([]database.WorkflowAgentTaskDetail, error) {
	return stub.details, stub.detailsErr
}

func (stub *workflowInspectorStub) Cancel(context.Context, string, string) (bool, error) {
	return true, nil
}
