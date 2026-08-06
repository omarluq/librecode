package di

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/workflow"
)

type capabilityAgentStub struct {
	err error
}

func (stub *capabilityAgentStub) SubmitAgentTask(
	context.Context,
	*assistant.AgentTaskRequest,
) (*database.AgentTaskEntity, error) {
	return testAgentTask(), stub.err
}

func (stub *capabilityAgentStub) Get(
	context.Context,
	string,
) (*database.AgentTaskEntity, bool, error) {
	return testAgentTask(), true, stub.err
}

func (stub *capabilityAgentStub) List(
	context.Context,
	string,
	int,
) ([]database.AgentTaskEntity, error) {
	return []database.AgentTaskEntity{*testAgentTask()}, stub.err
}

func (stub *capabilityAgentStub) Cancel(
	context.Context,
	string,
	string,
) (*database.TaskEntity, bool, error) {
	return testTask("task"), true, stub.err
}

func (stub *capabilityAgentStub) Await(
	context.Context,
	string,
) (*database.AgentTaskEntity, error) {
	return testAgentTask(), stub.err
}

func (stub *capabilityAgentStub) SubscribeAgentTask(
	string,
) (events <-chan database.TaskEventEntity, cancel func(), err error) {
	if stub.err != nil {
		return nil, nil, stub.err
	}

	channel := make(chan database.TaskEventEntity)
	close(channel)

	return channel, func() {}, nil
}

func (stub *capabilityAgentStub) Events(
	context.Context,
	string,
	int64,
	int,
) ([]database.TaskEventEntity, error) {
	return []database.TaskEventEntity{*testTaskEvent()}, stub.err
}

type capabilityWorkflowStub struct {
	err error
}

func (stub *capabilityWorkflowStub) Submit(
	context.Context,
	*workflow.ServiceRequest,
) (*database.WorkflowRunEntity, error) {
	return testWorkflowRun(), stub.err
}

func TestRuntimeCapabilitiesDelegateSuccessfulOperations(t *testing.T) {
	t.Parallel()

	capabilities := newRuntimeCapabilities()
	require.NoError(t, capabilities.publish(new(capabilityAgentStub), new(capabilityWorkflowStub)))

	submitted, err := capabilities.SubmitAgentTask(t.Context(), new(assistant.AgentTaskRequest))
	require.NoError(t, err)
	assert.Equal(t, "agent-task", submitted.Task.ID)

	got, found, err := capabilities.Get(t.Context(), "agent-task")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "agent-task", got.Task.ID)

	listed, err := capabilities.List(t.Context(), "owner", 1)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, "agent-task", listed[0].Task.ID)

	canceled, found, err := capabilities.Cancel(t.Context(), "owner", "task")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "task", canceled.ID)

	awaited, err := capabilities.Await(t.Context(), "agent-task")
	require.NoError(t, err)
	assert.Equal(t, "agent-task", awaited.Task.ID)

	subscription, cancel, err := capabilities.SubscribeAgentTask("agent-task")
	require.NoError(t, err)
	require.NotNil(t, cancel)
	cancel()

	_, open := <-subscription
	assert.False(t, open)

	events, err := capabilities.Events(t.Context(), "task", 0, 1)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, int64(1), events[0].Sequence)

	run, err := capabilities.Submit(t.Context(), new(workflow.ServiceRequest))
	require.NoError(t, err)
	assert.Equal(t, "workflow", run.Task.ID)
}

func TestRuntimeCapabilitiesRejectAgentEventsWithoutReader(t *testing.T) {
	t.Parallel()

	capabilities := newRuntimeCapabilities()
	require.NoError(t, capabilities.publish(
		&capabilityAgentWithoutEvents{AgentTaskController: new(capabilityAgentStub)},
		new(capabilityWorkflowStub),
	))

	_, err := capabilities.Events(t.Context(), "task", 0, 1)
	requireOopsCode(t, err, "agent_task_events_unavailable")
}

type capabilityAgentWithoutEvents struct {
	assistant.AgentTaskController
}

type capabilityErrorTest struct {
	call     func(*runtimeCapabilities) error
	name     string
	wantCode string
}

func TestRuntimeCapabilitiesWrapDelegationErrors(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("operation failed")

	for _, test := range runtimeCapabilityErrorTests(t) {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			capabilities := newRuntimeCapabilities()
			require.NoError(t, capabilities.publish(
				&capabilityAgentStub{err: expectedErr},
				&capabilityWorkflowStub{err: expectedErr},
			))

			err := test.call(capabilities)
			require.ErrorIs(t, err, expectedErr)
			requireOopsCode(t, err, test.wantCode)
		})
	}
}

func runtimeCapabilityErrorTests(t *testing.T) []capabilityErrorTest {
	t.Helper()

	return []capabilityErrorTest{
		{
			name: "submit agent task", wantCode: "submit_agent_task",
			call: func(capabilities *runtimeCapabilities) error {
				_, err := capabilities.SubmitAgentTask(t.Context(), new(assistant.AgentTaskRequest))

				return err
			},
		},
		{
			name: "get agent task", wantCode: "get_agent_task",
			call: func(capabilities *runtimeCapabilities) error {
				_, _, err := capabilities.Get(t.Context(), "task")

				return err
			},
		},
		{
			name: "list agent tasks", wantCode: "list_agent_tasks",
			call: func(capabilities *runtimeCapabilities) error {
				_, err := capabilities.List(t.Context(), "owner", 1)

				return err
			},
		},
		{
			name: "cancel agent task", wantCode: "cancel_agent_task",
			call: func(capabilities *runtimeCapabilities) error {
				_, _, err := capabilities.Cancel(t.Context(), "owner", "task")

				return err
			},
		},
		{
			name: "await agent task", wantCode: "await_agent_task",
			call: func(capabilities *runtimeCapabilities) error {
				_, err := capabilities.Await(t.Context(), "task")

				return err
			},
		},
		{
			name: "subscribe agent task", wantCode: "subscribe_agent_task",
			call: func(capabilities *runtimeCapabilities) error {
				_, _, err := capabilities.SubscribeAgentTask("task")

				return err
			},
		},
		{
			name: "list agent task events", wantCode: "list_agent_task_events",
			call: func(capabilities *runtimeCapabilities) error {
				_, err := capabilities.Events(t.Context(), "task", 0, 1)

				return err
			},
		},
		{
			name: "submit workflow", wantCode: "submit_workflow",
			call: func(capabilities *runtimeCapabilities) error {
				_, err := capabilities.Submit(t.Context(), new(workflow.ServiceRequest))

				return err
			},
		},
	}
}

func testAgentTask() *database.AgentTaskEntity {
	return &database.AgentTaskEntity{
		Task: *testTask("agent-task"), ChildSessionID: "", AgentName: "", Prompt: "", Model: "", Provider: "",
		PolicyJSON: "", UsageJSON: "", Depth: 0,
	}
}

func testTask(id string) *database.TaskEntity {
	return &database.TaskEntity{
		CreatedAt: time.Time{}, StartedAt: nil, FinishedAt: nil, UpdatedAt: time.Time{}, LeaseExpiresAt: nil,
		ID: id, Kind: "", ParentTaskID: "", OwnerSessionID: "", ConcurrencyKey: "", LeaseOwner: "",
		State: "", Result: "", ErrorCode: "", ErrorMessage: "",
	}
}

func testTaskEvent() *database.TaskEventEntity {
	return &database.TaskEventEntity{
		Event:  database.EventEntity{CreatedAt: time.Time{}, ID: "", Kind: "", PayloadJSON: ""},
		TaskID: "task", Sequence: 1,
	}
}

func testWorkflowRun() *database.WorkflowRunEntity {
	return &database.WorkflowRunEntity{
		Task: *testTask("workflow"), Name: "", Source: "", SourceHash: "", SourceVersion: "", ArgumentsJSON: "",
	}
}
