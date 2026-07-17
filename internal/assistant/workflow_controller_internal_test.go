package assistant

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/workflow"
)

const workflowControllerOwnerID = "workflow-controller-owner"

type workflowControllerTaskStub struct {
	task    *database.AgentTaskEntity
	request *AgentTaskRequest
	err     error
	listed  []database.AgentTaskEntity
	found   bool
}

func (stub *workflowControllerTaskStub) SubmitAgentTask(
	_ context.Context,
	request *AgentTaskRequest,
) (*database.AgentTaskEntity, error) {
	stub.request = request

	return stub.task, stub.err
}

func (stub *workflowControllerTaskStub) Get(
	context.Context,
	string,
) (*database.AgentTaskEntity, bool, error) {
	return stub.task, stub.found, stub.err
}

func (stub *workflowControllerTaskStub) List(
	context.Context,
	string,
	int,
) ([]database.AgentTaskEntity, error) {
	return stub.listed, stub.err
}

func (stub *workflowControllerTaskStub) Cancel(
	context.Context,
	string,
	string,
) (*database.TaskEntity, bool, error) {
	if stub.task == nil {
		return nil, stub.found, stub.err
	}

	return &stub.task.Task, stub.found, stub.err
}

func (stub *workflowControllerTaskStub) Await(
	context.Context,
	string,
) (*database.AgentTaskEntity, error) {
	return stub.task, stub.err
}

func (*workflowControllerTaskStub) SubscribeAgentTask(
	string,
) (events <-chan database.TaskEventEntity, unsubscribe func()) {
	eventChannel := make(chan database.TaskEventEntity)
	close(eventChannel)

	return eventChannel, func() {}
}

func TestNewWorkflowControllerValidatesDependencies(t *testing.T) {
	t.Parallel()

	tasks := new(workflowControllerTaskStub)
	sessions := agentToolSessions(t)
	submitter, err := NewAgentSubmitter(tasks, isolatedAgentCatalog(t))
	require.NoError(t, err)

	for _, testCase := range []struct {
		tasks     AgentTaskController
		submitter *AgentSubmitter
		sessions  *database.SessionRepository
		name      string
	}{
		{name: "nil submitter", tasks: tasks, submitter: nil, sessions: sessions},
		{name: "nil tasks", tasks: nil, submitter: submitter, sessions: sessions},
		{name: "nil sessions", tasks: tasks, submitter: submitter, sessions: nil},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, controllerErr := NewWorkflowController(testCase.submitter, testCase.tasks, testCase.sessions)
			assertOopsCode(t, controllerErr, "invalid_workflow_controller_dependencies")
		})
	}

	controller, err := NewWorkflowController(submitter, tasks, sessions)
	require.NoError(t, err)
	assert.Same(t, submitter, controller.submitter)
	assert.Same(t, tasks, controller.tasks)
	assert.Same(t, sessions, controller.sessions)
}

func TestWorkflowControllerSubmitResolvesOwnerAndPreservesRequestIdentity(t *testing.T) {
	t.Parallel()

	tasks := &workflowControllerTaskStub{
		err:     nil,
		task:    agentToolTask("workflow-agent-task", workflowControllerOwnerID, database.TaskQueued),
		request: nil,
		listed:  nil,
		found:   true,
	}
	sessions := agentToolSessions(t)
	owner, err := sessions.CreateSession(t.Context(), t.TempDir(), "owner", "")
	require.NoError(t, err)
	submitter, err := NewAgentSubmitter(tasks, isolatedAgentCatalog(t))
	require.NoError(t, err)
	controller, err := NewWorkflowController(submitter, tasks, sessions)
	require.NoError(t, err)

	request := &workflow.AgentRequest{
		ParentTaskID: "parent-task", OwnerSessionID: owner.ID, NodeKey: "node-1", Prompt: "review",
		Options: workflow.AgentOptions{
			NodeKey: "option-node", AgentName: defaultWorkflowAgentName, Model: "model", Provider: "provider",
			ConcurrencyKey: "concurrency", Depth: 2,
		},
		InvocationIndex: 3,
	}

	task, err := controller.Submit(t.Context(), request)
	require.NoError(t, err)
	assert.Same(t, tasks.task, task)
	require.NotNil(t, tasks.request)
	assert.Equal(t, request.ParentTaskID, tasks.request.ParentTaskID)
	assert.Equal(t, request.OwnerSessionID, tasks.request.OwnerSessionID)
	assert.Equal(t, request.NodeKey, tasks.request.NodeKey)
	assert.Equal(t, request.InvocationIndex, tasks.request.InvocationIndex)
	assert.Equal(t, owner.CWD, tasks.request.ChildSessionCWD)
}

func TestWorkflowControllerSubmitErrors(t *testing.T) {
	t.Parallel()

	tasks := new(workflowControllerTaskStub)
	sessions, databaseConnection := agentToolSessionsWithDB(t)
	submitter, err := NewAgentSubmitter(tasks, isolatedAgentCatalog(t))
	require.NoError(t, err)
	controller, err := NewWorkflowController(submitter, tasks, sessions)
	require.NoError(t, err)

	_, err = controller.Submit(t.Context(), nil)
	assertOopsCode(t, err, "nil_workflow_agent_request")
	_, err = controller.Submit(t.Context(), emptyWorkflowAgentRequest(runtimeSlashMissing))
	assertOopsCode(t, err, "workflow_owner_not_found")
	require.NoError(t, databaseConnection.Close())
	_, err = controller.Submit(t.Context(), emptyWorkflowAgentRequest("owner"))
	assertOopsCode(t, err, "get_session")
	assert.ErrorContains(t, err, "load workflow owner session")
}

func TestWorkflowControllerDelegatesTaskOperations(t *testing.T) {
	t.Parallel()

	task := agentToolTask("task-1", workflowControllerOwnerID, database.TaskRunning)
	listed := []database.AgentTaskEntity{*task}
	tasks := &workflowControllerTaskStub{err: nil, task: task, request: nil, listed: listed, found: true}
	controller := &WorkflowController{submitter: nil, tasks: tasks, sessions: nil}

	got, found, err := controller.Get(t.Context(), task.Task.ID)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Same(t, task, got)
	gotList, err := controller.List(t.Context(), workflowControllerOwnerID, 1)
	require.NoError(t, err)
	assert.Equal(t, listed, gotList)
	awaited, err := controller.Await(t.Context(), task.Task.ID)
	require.NoError(t, err)
	assert.Same(t, task, awaited)
	canceled, found, err := controller.Cancel(t.Context(), workflowControllerOwnerID, task.Task.ID)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Same(t, &task.Task, canceled)
}

func TestWorkflowControllerWrapsTaskOperationErrors(t *testing.T) {
	t.Parallel()

	tasks := &workflowControllerTaskStub{
		err: errors.New("backend failed"), task: nil, request: nil, listed: nil, found: false,
	}
	controller := &WorkflowController{submitter: nil, tasks: tasks, sessions: nil}

	_, _, err := controller.Get(t.Context(), "task")
	assertOopsCode(t, err, "get_workflow_agent_task")
	_, err = controller.List(t.Context(), workflowControllerOwnerID, 1)
	assertOopsCode(t, err, "list_workflow_agent_tasks")
	_, err = controller.Await(t.Context(), "task")
	assertOopsCode(t, err, "await_workflow_agent_task")
	_, _, err = controller.Cancel(t.Context(), workflowControllerOwnerID, "task")
	assertOopsCode(t, err, "cancel_workflow_agent_task")
}

func emptyWorkflowAgentRequest(owner string) *workflow.AgentRequest {
	return &workflow.AgentRequest{
		ParentTaskID: "", OwnerSessionID: owner, NodeKey: "", Prompt: "",
		Options: workflow.AgentOptions{
			NodeKey: "", AgentName: "", Model: "", Provider: "", ConcurrencyKey: "", Depth: 0,
		},
		InvocationIndex: 0,
	}
}

func assertOopsCode(t *testing.T, err error, want string) {
	t.Helper()
	require.Error(t, err)

	var oopsErr oops.OopsError
	require.ErrorAs(t, err, &oopsErr)
	assert.Equal(t, want, oopsErr.Code())
}
