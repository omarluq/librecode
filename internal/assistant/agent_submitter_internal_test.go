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

func TestAgentSubmitterCreatesChildAndSnapshotsPolicy(t *testing.T) {
	t.Parallel()

	controller := newAgentControllerStub(agentToolTask("task-1", "owner", database.TaskQueued), nil, true)
	sessions := agentToolSessions(t)
	cwd := t.TempDir()
	parent, err := sessions.CreateSession(t.Context(), cwd, "parent", "")
	require.NoError(t, err)
	submitter, err := NewAgentSubmitter(controller, sessions, isolatedAgentCatalog(t))
	require.NoError(t, err)

	task, err := submitter.SubmitAgent(t.Context(), &AgentSubmitRequest{
		ParentTaskID:   "",
		OwnerSessionID: parent.ID, CWD: cwd, AgentName: "", Prompt: "inspect code",
		Model: "model-override", Provider: "provider-override", ConcurrencyKey: parent.ID,
		NodeKey: "", InvocationIndex: 0, Depth: 2,
	})
	require.NoError(t, err)
	require.NotNil(t, task)
	require.NotNil(t, controller.lastSubmit)
	assert.Equal(t, defaultWorkflowAgentName, controller.lastSubmit.AgentName)
	assert.Equal(t, "model-override", controller.lastSubmit.Model)
	assert.Equal(t, "provider-override", controller.lastSubmit.Provider)
	assert.Equal(t, 2, controller.lastSubmit.Depth)
	assert.NotEmpty(t, controller.lastSubmit.ChildSessionID)

	child, found, err := sessions.GetSession(t.Context(), controller.lastSubmit.ChildSessionID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, parent.ID, child.ParentSession)

	var definition agent.Definition
	require.NoError(t, json.Unmarshal([]byte(controller.lastSubmit.PolicyJSON), &definition))
	assert.Equal(t, defaultWorkflowAgentName, definition.Name)
	assert.Equal(t, "model-override", definition.Model.Model)
	assert.Equal(t, "provider-override", definition.Model.Provider)
}

func TestAgentSubmitterCleansChildAfterRejectedSubmission(t *testing.T) {
	t.Parallel()

	controller := newAgentControllerStub(nil, nil, false)
	controller.submitErr = errors.New("rejected")
	sessions := agentToolSessions(t)
	cwd := t.TempDir()
	parent, err := sessions.CreateSession(t.Context(), cwd, "parent", "")
	require.NoError(t, err)
	submitter, err := NewAgentSubmitter(controller, sessions, isolatedAgentCatalog(t))
	require.NoError(t, err)

	_, err = submitter.SubmitAgent(t.Context(), &AgentSubmitRequest{
		ParentTaskID:   "",
		OwnerSessionID: parent.ID, CWD: cwd, AgentName: defaultWorkflowAgentName, Prompt: "inspect",
		Model: "", Provider: "", ConcurrencyKey: parent.ID, NodeKey: "", InvocationIndex: 0, Depth: 1,
	})
	require.Error(t, err)
	require.NotNil(t, controller.lastSubmit)

	_, found, getErr := sessions.GetSession(t.Context(), controller.lastSubmit.ChildSessionID)
	require.NoError(t, getErr)
	assert.False(t, found)
}
