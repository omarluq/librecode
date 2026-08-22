package assistant

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/guestapi"
	"github.com/omarluq/librecode/internal/tool"
	"github.com/omarluq/librecode/internal/workflow"
)

const (
	workflowTestSessionID = "workflow-session"
	workflowTestRunName   = "durable-review"
)

type workflowSubmitterStub struct {
	request *workflow.ServiceRequest
	run     *database.WorkflowRunEntity
	err     error
}

func (stub *workflowSubmitterStub) Submit(
	_ context.Context,
	request *workflow.ServiceRequest,
) (*database.WorkflowRunEntity, error) {
	stub.request = request

	return stub.run, stub.err
}

func TestExecuteDurableSubmitsModelAuthoredSource(t *testing.T) {
	t.Parallel()

	stub := &workflowSubmitterStub{
		request: nil,
		run: &database.WorkflowRunEntity{
			Task: workflowTestTask("run-1", workflowTestSessionID),
			Name: "review", Source: "", SourceHash: "", GuestAPIVersion: "", SourceVersion: "", ArgumentsJSON: "",
		},
		err: nil,
	}
	executor := newExecuteFacade(nil, nil, stub, workflowTestSessionID)
	input, err := tool.ArgumentsFromRaw([]byte(`{
		"profile":"durable","name":"review",
		"source":"import \"librecode/agents\"; agents.List()",
		"arguments":{"scope":"changes"}
	}`))
	require.NoError(t, err)

	result, err := executor.Execute(t.Context(), input)
	require.NoError(t, err)
	assert.Equal(t, `Started workflow "review" with run ID run-1.`, result.Text())
	assert.Equal(t, "run-1", result.Details["run_id"])
	assert.Equal(t, ExecutionResultAccepted, result.Details[executionResultKindKey])
	assert.Equal(t, MVMExecutionProfileDurable, result.Details[executionProfileKey])
	require.NotNil(t, stub.request)
	assert.Equal(t, workflowTestSessionID, stub.request.OwnerSessionID)
	assert.Equal(t, "review", stub.request.Name)
	assert.Equal(t, "v1", stub.request.SourceVersion)
	assert.Equal(t, guestapi.Version2, stub.request.GuestAPIVersion)
	assert.JSONEq(t, `{"scope":"changes"}`, stub.request.ArgumentsJSON)
}

func TestExecuteDurableReturnsWithoutAwaitingCompletion(t *testing.T) {
	t.Parallel()

	stub := &workflowSubmitterStub{
		request: nil,
		run: &database.WorkflowRunEntity{
			Task: workflowTestTask("run-queued", ""),
			Name: "background review", Source: "", SourceHash: "", GuestAPIVersion: "",
			SourceVersion: "", ArgumentsJSON: "",
		},
		err: nil,
	}
	executor := newExecuteFacade(nil, nil, stub, workflowTestSessionID)
	input, err := tool.ArgumentsFromRaw([]byte(
		`{"profile":"durable","name":"background review","source":"1 + 1"}`,
	))
	require.NoError(t, err)

	result, err := executor.Execute(t.Context(), input)
	require.NoError(t, err)
	assert.Equal(t, database.TaskQueued, result.Details["state"])
}

func TestExecuteDurableRejectsUnavailableAndSubmissionFailure(t *testing.T) {
	t.Parallel()

	unavailable := newExecuteFacade(nil, nil, nil, workflowTestSessionID)
	input, err := tool.ArgumentsFromRaw([]byte(`{"profile":"durable","source":"1"}`))
	require.NoError(t, err)
	result, err := unavailable.Execute(t.Context(), input)
	requireRuntimeOopsCode(t, err, "execute_durable_unavailable")
	assert.Equal(t, ExecutionResultRejected, result.Details[executionResultKindKey])
	assert.Equal(t, MVMExecutionProfileDurable, result.Details[executionProfileKey])

	failing := newExecuteFacade(nil, nil, &workflowSubmitterStub{
		request: nil, run: nil, err: errors.New("down"),
	}, workflowTestSessionID)
	result, err = failing.Execute(t.Context(), input)
	requireRuntimeOopsCode(t, err, "submit_workflow")
	assert.Equal(t, ExecutionResultFailed, result.Details[executionResultKindKey])
}

func TestPromptRegistryExposesOnlyUnifiedExecuteWithDurableAvailability(t *testing.T) {
	t.Parallel()

	stub := &workflowSubmitterStub{
		request: nil,
		run: &database.WorkflowRunEntity{
			Task: workflowTestTask("workflow-run", ""),
			Name: workflowTestRunName, Source: "", SourceHash: "", GuestAPIVersion: "",
			SourceVersion: "", ArgumentsJSON: "",
		},
		err: nil,
	}
	runtime := NewRuntimeForTest(func(options *RuntimeTestOptions) { options.WorkflowSubmitter = stub })
	registry, err := runtime.promptToolRegistry(t.Context(), t.TempDir(), "owner")
	require.NoError(t, err)
	assert.True(t, registry.Has(executeToolName))
	assert.False(t, registry.Has("workflow"))

	result, err := registry.Execute(t.Context(), string(executeToolName), mustArguments(t,
		`{"profile":"durable","source":"1"}`,
	))
	require.NoError(t, err)
	assert.Equal(t, ExecutionResultAccepted, result.Details[executionResultKindKey])

	direct, err := runtime.promptToolRegistry(
		WithToolStrategy(t.Context(), ToolStrategyDirect), t.TempDir(), "owner",
	)
	require.NoError(t, err)
	assert.False(t, direct.Has(executeToolName))
	assert.False(t, direct.Has("workflow"))
}

func workflowTestTask(id, owner string) database.TaskEntity {
	return database.TaskEntity{
		CreatedAt: time.Time{}, StartedAt: nil, FinishedAt: nil, UpdatedAt: time.Time{}, LeaseExpiresAt: nil,
		ID: id, Kind: database.TaskKindWorkflow, ParentTaskID: "", OwnerSessionID: owner,
		ConcurrencyKey: "", LeaseOwner: "", State: database.TaskQueued, Result: "", ErrorCode: "", ErrorMessage: "",
	}
}

func TestWorkflowResultDetailsHandlesNil(t *testing.T) {
	t.Parallel()
	assert.Empty(t, workflowResultDetails(nil))
}
