package assistant

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/tool"
	"github.com/omarluq/librecode/internal/workflow"
)

const workflowTestSessionID = "workflow-session"

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

func TestWorkflowToolSubmitsModelAuthoredSource(t *testing.T) {
	t.Parallel()

	stub := &workflowSubmitterStub{
		request: nil,
		run: &database.WorkflowRunEntity{
			Task: database.TaskEntity{
				CreatedAt: time.Time{}, StartedAt: nil, FinishedAt: nil, UpdatedAt: time.Time{},
				LeaseExpiresAt: nil, ID: "run-1", Kind: database.TaskKindWorkflow, ParentTaskID: "",
				OwnerSessionID: workflowTestSessionID, ConcurrencyKey: "", LeaseOwner: "",
				State: database.TaskSucceeded, Result: "reviewed", ErrorCode: "", ErrorMessage: "",
			},
			Name: "review", Source: "", SourceHash: "", SourceVersion: "", ArgumentsJSON: "",
		},
		err: nil,
	}
	executor := &workflowToolExecutor{submitter: stub, ownerSessionID: workflowTestSessionID}
	input, err := tool.ArgumentsFromRaw([]byte(`{
		"name":"review",
		"source":"import \"librecode/workflow\"; workflow.List()",
		"arguments":{"scope":"changes"}
	}`))
	require.NoError(t, err)

	result, err := executor.Execute(t.Context(), input)
	require.NoError(t, err)
	assert.Equal(t, `Started workflow "review" with run ID run-1.`, result.Text())
	assert.Equal(t, "run-1", result.Details["run_id"])
	assert.Equal(t, "review", result.Details["name"])
	require.NotNil(t, stub.request)
	assert.Equal(t, workflowTestSessionID, stub.request.OwnerSessionID)
	assert.Equal(t, "review", stub.request.Name)
	assert.JSONEq(t, `{"scope":"changes"}`, stub.request.ArgumentsJSON)
}

func TestWorkflowToolReturnsWithoutAwaitingCompletion(t *testing.T) {
	t.Parallel()

	stub := &workflowSubmitterStub{
		request: nil,
		run: &database.WorkflowRunEntity{
			Task: database.TaskEntity{
				CreatedAt: time.Time{}, StartedAt: nil, FinishedAt: nil, UpdatedAt: time.Time{},
				LeaseExpiresAt: nil, ID: "run-queued", Kind: database.TaskKindWorkflow, ParentTaskID: "",
				OwnerSessionID: workflowTestSessionID, ConcurrencyKey: "", LeaseOwner: "",
				State: database.TaskQueued, Result: "", ErrorCode: "", ErrorMessage: "",
			},
			Name: "background review", Source: "", SourceHash: "", SourceVersion: "", ArgumentsJSON: "",
		},
		err: nil,
	}
	executor := &workflowToolExecutor{submitter: stub, ownerSessionID: workflowTestSessionID}
	input, err := tool.ArgumentsFromRaw([]byte(`{"name":"background review","source":"1 + 1"}`))
	require.NoError(t, err)

	result, err := executor.Execute(t.Context(), input)
	require.NoError(t, err)
	assert.Equal(t, `Started workflow "background review" with run ID run-queued.`, result.Text())
	assert.Equal(t, database.TaskQueued, result.Details["state"])
}

func TestWorkflowToolRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	validStub := &workflowSubmitterStub{request: nil, run: nil, err: nil}

	tests := []struct {
		executor *workflowToolExecutor
		raw      string
		name     string
		want     string
	}{
		{
			name: "missing submitter", executor: &workflowToolExecutor{submitter: nil, ownerSessionID: ""},
			raw: `{}`, want: "workflow service is unavailable",
		},
		{
			name: "invalid shape", executor: &workflowToolExecutor{submitter: validStub, ownerSessionID: ""},
			raw: `{"name":1}`, want: "decode workflow input",
		},
		{
			name: "blank name", executor: &workflowToolExecutor{submitter: validStub, ownerSessionID: ""},
			raw: `{"name":" ","source":"1"}`, want: "workflow name is required",
		},
		{
			name: "blank source", executor: &workflowToolExecutor{submitter: validStub, ownerSessionID: ""},
			raw: `{"name":"run","source":" "}`, want: "workflow source is required",
		},
		{
			name: "submit failure",
			executor: &workflowToolExecutor{
				submitter:      &workflowSubmitterStub{request: nil, run: nil, err: errors.New("down")},
				ownerSessionID: "",
			},
			raw: `{"name":"run","source":"1"}`, want: "submit workflow",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			input, err := tool.ArgumentsFromRaw([]byte(test.raw))
			require.NoError(t, err)
			_, err = test.executor.Execute(t.Context(), input)
			require.ErrorContains(t, err, test.want)
		})
	}
}

func TestWorkflowResultDetailsHandlesNil(t *testing.T) {
	t.Parallel()

	assert.Empty(t, workflowResultDetails(nil))
}

func TestPromptRegistryIncludesWorkflowWhenConfigured(t *testing.T) {
	t.Parallel()

	runtime := new(Runtime)
	runtime.profile = topLevelExecutionProfile()
	runtime.SetWorkflowSubmitter(&workflowSubmitterStub{request: nil, run: nil, err: nil})

	registry, err := runtime.promptToolRegistry(t.Context(), t.TempDir(), "owner")
	require.NoError(t, err)
	assert.True(t, registry.Has(workflowToolName))

	runtime.SetWorkflowSubmitter(nil)
	registry, err = runtime.promptToolRegistry(t.Context(), t.TempDir(), "owner")
	require.NoError(t, err)
	assert.False(t, registry.Has(workflowToolName))
}
