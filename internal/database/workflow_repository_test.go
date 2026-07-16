package database_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

func TestWorkflowRepositoryLifecycleAndAgentLinks(t *testing.T) {
	t.Parallel()

	fixture := newTaskTestFixture(t)
	ctx := t.Context()
	owner, firstChild := fixture.createAgentTaskSessions(ctx)
	secondChild, err := fixture.sessions.CreateSession(ctx, owner.CWD, "second", owner.ID)
	require.NoError(t, err)

	repository := fixture.workflows
	created, err := repository.Create(ctx, newWorkflowRun(owner.ID))
	require.NoError(t, err)
	assert.Equal(t, database.TaskKindWorkflow, created.Task.Kind)
	assert.Equal(t, database.TaskQueued, created.Task.State)

	loaded, found, err := repository.Get(ctx, created.Task.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, created.Name, loaded.Name)
	assert.Equal(t, created.Source, loaded.Source)
	assert.Equal(t, created.ArgumentsJSON, loaded.ArgumentsJSON)

	first, err := fixture.agents.Create(ctx, newAgentTask(owner.ID, firstChild.ID))
	require.NoError(t, err)
	second, err := fixture.agents.Create(ctx, newAgentTask(owner.ID, secondChild.ID))
	require.NoError(t, err)

	firstLink, err := repository.LinkAgentTask(ctx, created.Task.ID, first.Task.ID, "inspect", 0)
	require.NoError(t, err)
	secondLink, err := repository.LinkAgentTask(ctx, created.Task.ID, second.Task.ID, "review", 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), firstLink.Sequence)
	assert.Equal(t, int64(2), secondLink.Sequence)

	foundLink, found, err := repository.FindAgentTask(ctx, created.Task.ID, " inspect ", 0)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, first.Task.ID, foundLink.AgentTaskID)

	links, err := repository.ListAgentTasks(ctx, created.Task.ID)
	require.NoError(t, err)
	require.Len(t, links, 2)
	assert.Equal(t, []string{first.Task.ID, second.Task.ID}, []string{links[0].AgentTaskID, links[1].AgentTaskID})

	changed, err := repository.Tasks().Transition(ctx, created.Task.ID,
		[]database.TaskState{database.TaskQueued}, database.TaskRunning, taskStartedEvent)
	require.NoError(t, err)
	require.True(t, changed)

	finish := newTaskFinish(
		created.Task.ID, []database.TaskState{database.TaskRunning}, database.TaskSucceeded, taskSucceededEvent,
	)
	finish.Result = "complete"
	changed, err = repository.Tasks().Finish(ctx, &finish)
	require.NoError(t, err)
	require.True(t, changed)

	runs, err := repository.ListByOwner(ctx, owner.ID, 10)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, "complete", runs[0].Task.Result)
	events, err := repository.Tasks().ListEvents(ctx, created.Task.ID, 0, 10)
	require.NoError(t, err)
	assert.Len(t, events, 3)
}

func TestWorkflowRepositoryCreateAgentTaskAtomicallyLinks(t *testing.T) {
	t.Parallel()

	fixture := newTaskTestFixture(t)
	ctx := t.Context()
	owner, child := fixture.createAgentTaskSessions(ctx)
	run, err := fixture.workflows.Create(ctx, newWorkflowRun(owner.ID))
	require.NoError(t, err)

	agentTask := newAgentTask(owner.ID, child.ID)
	agentTask.Task.ParentTaskID = run.Task.ID
	created, err := fixture.workflows.CreateAgentTask(ctx, run.Task.ID, agentTask, "inspect", 0)
	require.NoError(t, err)

	link, found, err := fixture.workflows.FindAgentTask(ctx, run.Task.ID, "inspect", 0)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, created.Task.ID, link.AgentTaskID)

	invalid := newAgentTask(owner.ID, child.ID)
	invalid.Task.ParentTaskID = run.Task.ID
	_, err = fixture.workflows.CreateAgentTask(ctx, run.Task.ID, invalid, "inspect", 0)
	require.Error(t, err)

	listed, listErr := fixture.agents.ListByOwner(ctx, owner.ID, 10)
	require.NoError(t, listErr)
	assert.Len(t, listed, 1, "failed workflow links must roll back agent task creation")
}

func TestWorkflowRepositoryCreatesChildSessionAtomically(t *testing.T) {
	t.Parallel()

	fixture := newTaskTestFixture(t)
	ctx := t.Context()
	owner := fixture.createOwner(ctx)
	run, err := fixture.workflows.Create(ctx, newWorkflowRun(owner.ID))
	require.NoError(t, err)

	create := func(nodeKey string) (*database.AgentTaskEntity, error) {
		task := newAgentTask(owner.ID, "")
		task.Task.ParentTaskID = run.Task.ID

		return fixture.workflows.CreateAgentTaskWithChildSession(
			ctx,
			run.Task.ID,
			task,
			&database.ChildSessionRequest{CWD: owner.CWD, Name: nodeKey, ParentSessionID: owner.ID},
			"inspect",
			0,
		)
	}

	created, err := create("child")
	require.NoError(t, err)
	assert.NotEmpty(t, created.ChildSessionID)

	_, err = create("orphan")
	require.Error(t, err)

	children, err := fixture.sessions.ListChildSessions(ctx, owner.ID)
	require.NoError(t, err)
	assert.Len(t, children, 1, "link conflicts must roll back their child session")

	agentTasks, err := fixture.agents.ListByOwner(ctx, owner.ID, 10)
	require.NoError(t, err)
	assert.Len(t, agentTasks, 1, "link conflicts must roll back their agent task")

	links, err := fixture.workflows.ListAgentTasks(ctx, run.Task.ID)
	require.NoError(t, err)
	assert.Len(t, links, 1, "link conflicts must not add another workflow link")

	mismatched := newAgentTask(owner.ID, "")
	mismatched.Task.ParentTaskID = run.Task.ID
	_, err = fixture.workflows.CreateAgentTaskWithChildSession(
		ctx, run.Task.ID, mismatched,
		&database.ChildSessionRequest{CWD: owner.CWD, Name: "wrong-parent", ParentSessionID: testUUIDV7(t)},
		"other", 0,
	)
	require.ErrorContains(t, err, "parent differs from agent task owner")
}

func TestWorkflowRepositoryValidation(t *testing.T) {
	t.Parallel()

	const (
		testSource = "source"
		testHash   = "hash"
	)

	fixture := newTaskTestFixture(t)
	owner := fixture.createOwner(t.Context())
	repository := fixture.workflows

	tests := []struct {
		name      string
		run       *database.WorkflowRunEntity
		wantError string
	}{
		{name: "nil", run: nil, wantError: "workflow run is required"},
		{name: "source", run: workflowRunForValidation(owner.ID, "", testHash, "{}"),
			wantError: "source is required"},
		{name: "hash", run: workflowRunForValidation(owner.ID, testSource, "", "{}"),
			wantError: "source_hash is required"},
		{name: "arguments", run: workflowRunForValidation(owner.ID, testSource, testHash, "{"),
			wantError: "arguments_json must be valid JSON"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := repository.Create(t.Context(), test.run)
			require.ErrorContains(t, err, test.wantError)
		})
	}

	_, err := repository.LinkAgentTask(t.Context(), "bad", "bad", "", 0)
	require.ErrorContains(t, err, "workflow_task_id must be a UUIDv7")
}

func newWorkflowRun(ownerSessionID string) *database.WorkflowRunEntity {
	return &database.WorkflowRunEntity{
		Task: *newTask(ownerSessionID), Name: "inspect database", Source: "return agent('inspect')",
		SourceHash: "sha256:first", SourceVersion: "v1", ArgumentsJSON: `{"scope":"database"}`,
	}
}

func workflowRunForValidation(ownerSessionID, source, hash, arguments string) *database.WorkflowRunEntity {
	return &database.WorkflowRunEntity{
		Task: *newTask(ownerSessionID), Name: "validation", Source: source, SourceHash: hash, SourceVersion: "",
		ArgumentsJSON: arguments,
	}
}
