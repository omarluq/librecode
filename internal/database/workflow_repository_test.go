package database_test

import (
	"fmt"
	"path/filepath"
	"sync"
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
		testSource             = "source"
		testHash               = "hash"
		argumentsObjectMessage = "arguments_json must be a JSON object"
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
		{name: "arguments syntax", run: workflowRunForValidation(owner.ID, testSource, testHash, "{"),
			wantError: argumentsObjectMessage},
		{name: "arguments array", run: workflowRunForValidation(owner.ID, testSource, testHash, "[]"),
			wantError: argumentsObjectMessage},
		{name: "arguments null", run: workflowRunForValidation(owner.ID, testSource, testHash, "null"),
			wantError: argumentsObjectMessage},
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

	_, err = repository.CreateAgentTask(t.Context(), testUUIDV7(t), nil, "node", 0)
	require.ErrorContains(t, err, "agent task is required")
}

func TestWorkflowRepositoryLinkAgentTaskOnlyRecoversInvocationUniqueness(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	connection := openTestSQLite(t, filepath.Join(t.TempDir(), "workflow.db"), 0)
	require.NoError(t, database.Migrate(ctx, connection))
	sessions := database.NewSessionRepository(connection)
	workflows := database.NewWorkflowRepository(connection)
	agents := database.NewAgentTaskRepository(connection)

	owner, err := sessions.CreateSession(ctx, t.TempDir(), "owner", "")
	require.NoError(t, err)
	child, err := sessions.CreateSession(ctx, owner.CWD, "child", owner.ID)
	require.NoError(t, err)
	run, err := workflows.Create(ctx, newWorkflowRun(owner.ID))
	require.NoError(t, err)
	task, err := agents.Create(ctx, newAgentTask(owner.ID, child.ID))
	require.NoError(t, err)
	_, err = workflows.LinkAgentTask(ctx, run.Task.ID, task.Task.ID, "node", 0)
	require.NoError(t, err)

	_, err = connection.ExecContext(ctx, `CREATE TRIGGER reject_workflow_link BEFORE INSERT ON workflow_agent_tasks
BEGIN SELECT RAISE(ABORT, 'reject workflow link'); END`)
	require.NoError(t, err)

	_, err = workflows.LinkAgentTask(ctx, run.Task.ID, task.Task.ID, "node", 0)
	require.ErrorContains(t, err, "reject workflow link")
}

func TestWorkflowRepositoryLinkAgentTaskAssignsConcurrentUniqueSequences(t *testing.T) {
	t.Parallel()

	fixture := newTaskTestFixture(t)
	ctx := t.Context()
	owner := fixture.createOwner(ctx)
	run, err := fixture.workflows.Create(ctx, newWorkflowRun(owner.ID))
	require.NoError(t, err)

	const count = 8

	taskIDs := make([]string, count)
	for index := range count {
		child, createErr := fixture.sessions.CreateSession(ctx, owner.CWD, "child", owner.ID)
		require.NoError(t, createErr)
		task, createErr := fixture.agents.Create(ctx, newAgentTask(owner.ID, child.ID))
		require.NoError(t, createErr)

		taskIDs[index] = task.Task.ID
	}

	var wait sync.WaitGroup

	errors := make(chan error, count)
	for index := range count {
		wait.Go(func() {
			_, linkErr := fixture.workflows.LinkAgentTask(ctx, run.Task.ID, taskIDs[index], "node", index)
			errors <- linkErr
		})
	}

	wait.Wait()
	close(errors)

	for linkErr := range errors {
		require.NoError(t, linkErr)
	}

	links, err := fixture.workflows.ListAgentTasks(ctx, run.Task.ID)
	require.NoError(t, err)
	require.Len(t, links, count)

	for index := range links {
		assert.Equal(t, int64(index+1), links[index].Sequence)
	}
}

func TestWorkflowRepositoryBoundariesAndIdempotentLinks(t *testing.T) {
	t.Parallel()

	fixture := newTaskTestFixture(t)
	ctx := t.Context()
	owner, child := fixture.createAgentTaskSessions(ctx)
	run := newWorkflowRun(owner.ID)
	run.ArgumentsJSON = ""
	created, err := fixture.workflows.Create(ctx, run)
	require.NoError(t, err)
	assert.Equal(t, "{}", created.ArgumentsJSON)
	assert.NotNil(t, fixture.workflows.AgentTasks())

	task, err := fixture.agents.Create(ctx, newAgentTask(owner.ID, child.ID))
	require.NoError(t, err)
	first, err := fixture.workflows.LinkAgentTask(ctx, created.Task.ID, task.Task.ID, " node ", 0)
	require.NoError(t, err)
	repeated, err := fixture.workflows.LinkAgentTask(ctx, created.Task.ID, task.Task.ID, "node", 0)
	require.NoError(t, err)
	assert.Equal(t, first, repeated)

	otherChild, err := fixture.sessions.CreateSession(ctx, owner.CWD, "other", owner.ID)
	require.NoError(t, err)
	other, err := fixture.agents.Create(ctx, newAgentTask(owner.ID, otherChild.ID))
	require.NoError(t, err)
	_, err = fixture.workflows.LinkAgentTask(ctx, created.Task.ID, other.Task.ID, "node", 0)
	require.ErrorContains(t, err, "already linked")

	_, err = fixture.workflows.LinkAgentTask(ctx, created.Task.ID, "bad", "node", 1)
	require.ErrorContains(t, err, "agent_task_id must be a UUIDv7")
}

func TestWorkflowRepositoryCreateRollsBackEveryWriteOnFailure(t *testing.T) {
	t.Parallel()

	const workflowRunsTable = "workflow_runs"

	tests := []struct {
		name    string
		trigger string
	}{
		{
			name: "workflow metadata insert",
			trigger: `CREATE TRIGGER reject_workflow BEFORE INSERT ON workflow_runs
BEGIN SELECT RAISE(ABORT, 'reject workflow'); END`,
		},
		{
			name: "initial event insert",
			trigger: `CREATE TRIGGER reject_workflow_event BEFORE INSERT ON task_events
WHEN NEW.kind = 'task_queued'
BEGIN SELECT RAISE(ABORT, 'reject workflow event'); END`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			connection := openTestSQLite(t, filepath.Join(t.TempDir(), "workflow.db"), 0)
			require.NoError(t, database.Migrate(ctx, connection))
			sessions := database.NewSessionRepository(connection)
			owner, err := sessions.CreateSession(ctx, t.TempDir(), "owner", "")
			require.NoError(t, err)
			_, err = connection.ExecContext(ctx, test.trigger)
			require.NoError(t, err)

			_, err = database.NewWorkflowRepository(connection).Create(ctx, newWorkflowRun(owner.ID))
			require.Error(t, err)

			for _, table := range []string{"tasks", workflowRunsTable, "task_events"} {
				var count int

				err = connection.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count)
				require.NoError(t, err)
				assert.Zero(t, count, "%s write must be rolled back", table)
			}
		})
	}
}

func TestWorkflowRepositoryReportsStorageAndCorruptRowErrors(t *testing.T) {
	t.Parallel()

	const workflowRunsTable = "workflow_runs"

	t.Run("corrupt workflow timestamp", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		connection := openTestSQLite(t, filepath.Join(t.TempDir(), "workflow.db"), 0)
		require.NoError(t, database.Migrate(ctx, connection))

		sessions := database.NewSessionRepository(connection)
		owner, err := sessions.CreateSession(ctx, t.TempDir(), "owner", "")
		require.NoError(t, err)

		repository := database.NewWorkflowRepository(connection)
		run, err := repository.Create(ctx, newWorkflowRun(owner.ID))
		require.NoError(t, err)
		_, err = connection.ExecContext(ctx, `UPDATE tasks SET created_at = 'not-time' WHERE id = ?`, run.Task.ID)
		require.NoError(t, err)

		_, found, err := repository.Get(ctx, run.Task.ID)
		assert.False(t, found)
		require.ErrorContains(t, err, "parse timestamp")
	})

	tests := []struct {
		call  func(*database.WorkflowRepository) error
		name  string
		table string
	}{
		{call: func(repository *database.WorkflowRepository) error {
			_, _, err := repository.Get(t.Context(), testUUIDV7(t))

			return fmt.Errorf("get workflow: %w", err)
		}, name: "load one", table: workflowRunsTable},
		{call: func(repository *database.WorkflowRepository) error {
			_, err := repository.ListByOwner(t.Context(), testUUIDV7(t), 0)

			return fmt.Errorf("list workflows: %w", err)
		}, name: "list runs", table: workflowRunsTable},
		{call: func(repository *database.WorkflowRepository) error {
			_, err := repository.ListAgentTasks(t.Context(), testUUIDV7(t))

			return fmt.Errorf("list workflow links: %w", err)
		}, name: "list links", table: "workflow_agent_tasks"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			connection := openTestSQLite(t, filepath.Join(t.TempDir(), "workflow.db"), 0)
			require.NoError(t, database.Migrate(ctx, connection))
			_, err := connection.ExecContext(ctx, "DROP TABLE "+test.table)
			require.NoError(t, err)

			require.Error(t, test.call(database.NewWorkflowRepository(connection)))
		})
	}
}

func TestWorkflowRepositoryConcurrentIdenticalLinkIsIdempotent(t *testing.T) {
	t.Parallel()

	fixture := newTaskTestFixture(t)
	ctx := t.Context()
	owner, child := fixture.createAgentTaskSessions(ctx)
	run, err := fixture.workflows.Create(ctx, newWorkflowRun(owner.ID))
	require.NoError(t, err)
	agent, err := fixture.agents.Create(ctx, newAgentTask(owner.ID, child.ID))
	require.NoError(t, err)

	const count = 8

	var wait sync.WaitGroup

	errors := make(chan error, count)
	for range count {
		wait.Go(func() {
			_, linkErr := fixture.workflows.LinkAgentTask(ctx, run.Task.ID, agent.Task.ID, " same ", 3)
			errors <- linkErr
		})
	}

	wait.Wait()
	close(errors)

	for linkErr := range errors {
		require.NoError(t, linkErr)
	}

	links, err := fixture.workflows.ListAgentTasks(ctx, run.Task.ID)
	require.NoError(t, err)
	require.Len(t, links, 1)
	assert.Equal(t, int64(1), links[0].Sequence)
	assert.Equal(t, "same", links[0].NodeKey)
	assert.Equal(t, 3, links[0].InvocationIndex)
}

func TestWorkflowRepositoryRejectsAgentParentAndOwnerMismatch(t *testing.T) {
	t.Parallel()

	fixture := newTaskTestFixture(t)
	ctx := t.Context()
	owner, child := fixture.createAgentTaskSessions(ctx)
	run, err := fixture.workflows.Create(ctx, newWorkflowRun(owner.ID))
	require.NoError(t, err)

	wrongParent := newAgentTask(owner.ID, child.ID)
	wrongParent.Task.ParentTaskID = testUUIDV7(t)
	_, err = fixture.workflows.CreateAgentTask(ctx, run.Task.ID, wrongParent, "node", 0)
	require.ErrorContains(t, err, "agent task parent")

	otherOwner := fixture.createOwner(ctx)
	otherChild, err := fixture.sessions.CreateSession(ctx, otherOwner.CWD, "child", otherOwner.ID)
	require.NoError(t, err)

	wrongOwner := newAgentTask(otherOwner.ID, otherChild.ID)
	wrongOwner.Task.ParentTaskID = run.Task.ID
	_, err = fixture.workflows.CreateAgentTask(ctx, run.Task.ID, wrongOwner, "node", 0)
	require.ErrorContains(t, err, "owner differs")
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
