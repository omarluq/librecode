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
	getRun        *database.WorkflowRunEntity
	runs          []database.WorkflowRunEntity
	events        []database.TaskEventEntity
	children      []database.WorkflowAgentTaskEntity
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
