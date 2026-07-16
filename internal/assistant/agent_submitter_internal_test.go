package assistant

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/agent"
	"github.com/omarluq/librecode/internal/database"
)

const agentSubmitterOwnerID = "owner"

func TestAgentSubmitterBuildsAtomicChildSubmissionAndSnapshotsPolicy(t *testing.T) {
	t.Parallel()

	controller := newAgentControllerStub(agentToolTask("task-1", "owner", database.TaskQueued), nil, true)
	cwd := t.TempDir()
	submitter, err := NewAgentSubmitter(controller, isolatedAgentCatalog(t))
	require.NoError(t, err)

	task, err := submitter.SubmitAgent(t.Context(), &AgentSubmitRequest{
		ParentTaskID: "", OwnerSessionID: agentSubmitterOwnerID, CWD: cwd, AgentName: "", Prompt: "inspect code",
		Model: "model-override", Provider: "provider-override", ConcurrencyKey: agentSubmitterOwnerID,
		NodeKey: "", InvocationIndex: 0, Depth: 2,
	})
	require.NoError(t, err)
	require.NotNil(t, task)
	require.NotNil(t, controller.lastSubmit)
	assert.Equal(t, defaultWorkflowAgentName, controller.lastSubmit.AgentName)
	assert.Equal(t, "model-override", controller.lastSubmit.Model)
	assert.Equal(t, "provider-override", controller.lastSubmit.Provider)
	assert.Equal(t, 2, controller.lastSubmit.Depth)
	assert.Empty(t, controller.lastSubmit.ChildSessionID)
	assert.Equal(t, cwd, controller.lastSubmit.ChildSessionCWD)
	assert.NotEmpty(t, controller.lastSubmit.ChildSessionName)

	var definition agent.Definition
	require.NoError(t, json.Unmarshal([]byte(controller.lastSubmit.PolicyJSON), &definition))
	assert.Equal(t, defaultWorkflowAgentName, definition.Name)
	assert.Equal(t, "model-override", definition.Model.Model)
	assert.Equal(t, "provider-override", definition.Model.Provider)
}

func TestAgentSubmitterReturnsRejectedAtomicSubmission(t *testing.T) {
	t.Parallel()

	controller := newAgentControllerStub(nil, nil, false)
	controller.submitErr = errors.New("rejected")
	submitter, err := NewAgentSubmitter(controller, isolatedAgentCatalog(t))
	require.NoError(t, err)

	_, err = submitter.SubmitAgent(t.Context(), &AgentSubmitRequest{
		ParentTaskID: "", OwnerSessionID: agentSubmitterOwnerID, CWD: t.TempDir(),
		AgentName: defaultWorkflowAgentName, Prompt: "inspect", Model: "", Provider: "",
		ConcurrencyKey: agentSubmitterOwnerID, NodeKey: "", InvocationIndex: 0, Depth: 1,
	})
	require.Error(t, err)
	require.NotNil(t, controller.lastSubmit)
	assert.Empty(t, controller.lastSubmit.ChildSessionID)
	assert.NotEmpty(t, controller.lastSubmit.ChildSessionName)
}
