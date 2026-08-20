package database

import (
	"database/sql"
	"testing"
	"time"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vingarcia/ksql"
)

const (
	repositorySessionName   = "session"
	repositoryDocumentName  = "document"
	repositoryTaskName      = "task"
	repositoryToolTaskName  = "tool task"
	repositoryAgentTaskName = "agent task"
	repositoryWorkflowName  = "workflow"
)

type repositoryConstructorCase struct {
	construct func(*sql.DB) error
	name      string
}

type nilEntityCase struct {
	call func() error
	name string
	code string
}

type providerRepositoryConstructorCase struct {
	construct func(ksql.Provider) error
	name      string
}

func TestRepositoryConstructorsRejectInvalidSQLConnections(t *testing.T) {
	t.Parallel()

	constructors := []repositoryConstructorCase{
		{name: repositorySessionName, construct: sessionConstructorError},
		{name: repositoryDocumentName, construct: documentConstructorError},
		{name: repositoryTaskName, construct: taskConstructorError},
		{name: repositoryToolTaskName, construct: toolTaskConstructorError},
		{name: repositoryAgentTaskName, construct: agentTaskConstructorError},
		{name: repositoryWorkflowName, construct: workflowConstructorError},
	}

	for _, constructor := range constructors {
		t.Run(constructor.name, func(t *testing.T) {
			t.Parallel()

			assertRepositoryOopsCode(t, constructor.construct(nil), "nil_sql_connection")

			connection, err := sql.Open("sqlite", ":memory:")
			require.NoError(t, err)
			require.NoError(t, connection.Close())
			assertRepositoryOopsCode(t, constructor.construct(connection), "ping_sql_connection")
		})
	}
}

func TestRepositoryConstructorsRejectNilProviders(t *testing.T) {
	t.Parallel()

	constructors := []providerRepositoryConstructorCase{
		{name: repositorySessionName, construct: sessionProviderConstructorError},
		{name: repositoryDocumentName, construct: documentProviderConstructorError},
		{name: repositoryTaskName, construct: taskProviderConstructorError},
		{name: repositoryToolTaskName, construct: toolTaskProviderConstructorError},
		{name: repositoryAgentTaskName, construct: agentTaskProviderConstructorError},
		{name: repositoryWorkflowName, construct: workflowProviderConstructorError},
	}

	var typedNil *ksql.Mock

	providers := []struct {
		provider ksql.Provider
		name     string
	}{
		{name: "nil", provider: nil},
		{name: "typed nil", provider: typedNil},
	}

	for _, constructor := range constructors {
		for _, provider := range providers {
			t.Run(constructor.name+"/"+provider.name, func(t *testing.T) {
				t.Parallel()
				assertRepositoryOopsCode(t, constructor.construct(provider.provider), "nil_sql_provider")
			})
		}
	}
}

func TestCompositeRepositoryConstructorsShareDependencies(t *testing.T) {
	t.Parallel()

	connection, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })

	provider, err := newSQLProvider(connection)
	require.NoError(t, err)
	tasks, err := NewTaskRepositoryWithProvider(provider)
	require.NoError(t, err)
	agentTasks, err := NewAgentTaskRepositoryWithProvider(provider, tasks)
	require.NoError(t, err)
	workflows, err := NewWorkflowRepositoryWithProvider(provider, tasks, agentTasks)
	require.NoError(t, err)

	assert.Same(t, tasks, agentTasks.Tasks())
	assert.Same(t, tasks, workflows.Tasks())
	assert.Same(t, agentTasks, workflows.AgentTasks())

	fixedNow := func() time.Time { return time.Unix(123, 0) }
	tasks.now = fixedNow
	assert.Equal(t, fixedNow(), agentTasks.Tasks().now())
	assert.Equal(t, fixedNow(), workflows.Tasks().now())
}

func TestCompositeRepositoriesUseSharedClock(t *testing.T) {
	t.Parallel()

	connection, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	require.NoError(t, Migrate(t.Context(), connection))

	provider, err := newSQLProvider(connection)
	require.NoError(t, err)
	sessions, err := NewSessionRepositoryWithProvider(provider)
	require.NoError(t, err)
	tasks, err := NewTaskRepositoryWithProvider(provider)
	require.NoError(t, err)
	agentTasks, err := NewAgentTaskRepositoryWithProvider(provider, tasks)
	require.NoError(t, err)
	workflows, err := NewWorkflowRepositoryWithProvider(provider, tasks, agentTasks)
	require.NoError(t, err)

	fixedNow := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	tasks.now = func() time.Time { return fixedNow }

	owner, err := sessions.CreateSession(t.Context(), t.TempDir(), "owner", "")
	require.NoError(t, err)
	child, err := sessions.CreateSession(t.Context(), owner.CWD, "child", owner.ID)
	require.NoError(t, err)

	createdTask, err := tasks.Create(t.Context(), newClockTestTask(owner.ID, "test"))
	require.NoError(t, err)
	createdAgentTask, err := agentTasks.Create(t.Context(), &AgentTaskEntity{
		Task:           *newClockTestTask(owner.ID, ""),
		ChildSessionID: child.ID,
		AgentName:      "clock-agent",
		Prompt:         "inspect",
		Model:          "",
		Provider:       "",
		PolicyJSON:     "{}",
		UsageJSON:      "{}",
		Depth:          1,
	})
	require.NoError(t, err)
	createdWorkflow, err := workflows.Create(t.Context(), &WorkflowRunEntity{
		Task:          *newClockTestTask(owner.ID, ""),
		Name:          "clock workflow",
		Source:        "workflow source",
		SourceHash:    "source hash",
		SourceVersion: "1",
		ArgumentsJSON: "{}",
	})
	require.NoError(t, err)

	for _, created := range []*TaskEntity{createdTask, &createdAgentTask.Task, &createdWorkflow.Task} {
		assert.Equal(t, fixedNow, created.CreatedAt)
		assert.Equal(t, fixedNow, created.UpdatedAt)
	}
}

func newClockTestTask(ownerSessionID, kind string) *TaskEntity {
	return &TaskEntity{
		CreatedAt:      time.Time{},
		StartedAt:      nil,
		FinishedAt:     nil,
		UpdatedAt:      time.Time{},
		LeaseExpiresAt: nil,
		ID:             "",
		Kind:           kind,
		ParentTaskID:   "",
		OwnerSessionID: ownerSessionID,
		ConcurrencyKey: "",
		LeaseOwner:     "",
		State:          "",
		Result:         "",
		ErrorCode:      "",
		ErrorMessage:   "",
	}
}

func TestCompositeRepositoryConstructorsRejectInvalidGraph(t *testing.T) {
	t.Parallel()

	connection, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })

	provider, err := newSQLProvider(connection)
	require.NoError(t, err)
	tasks, err := NewTaskRepositoryWithProvider(provider)
	require.NoError(t, err)
	otherTasks, err := NewTaskRepositoryWithProvider(provider)
	require.NoError(t, err)
	agentTasks, err := NewAgentTaskRepositoryWithProvider(provider, tasks)
	require.NoError(t, err)

	otherConnection, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, otherConnection.Close()) })

	otherProvider, err := newSQLProvider(otherConnection)
	require.NoError(t, err)

	const graphMismatch = "repository_graph_mismatch"

	tests := map[string]struct {
		err  error
		code string
	}{
		"agent task with nil task repository": {
			err:  agentTaskGraphConstructorError(provider, nil),
			code: "nil_task_repository",
		},
		"agent task with mismatched provider": {
			err:  agentTaskGraphConstructorError(otherProvider, tasks),
			code: graphMismatch,
		},
		"tool task with nil task repository": {
			err:  toolTaskGraphConstructorError(provider, nil),
			code: graphMismatch,
		},
		"tool task with mismatched provider": {
			err:  toolTaskGraphConstructorError(otherProvider, tasks),
			code: graphMismatch,
		},
		"workflow with nil task repository": {
			err:  workflowGraphConstructorError(provider, nil, agentTasks),
			code: "nil_task_repository",
		},
		"workflow with nil agent task repository": {
			err:  workflowGraphConstructorError(provider, tasks, nil),
			code: "nil_agent_task_repository",
		},
		"workflow with mismatched task repository": {
			err:  workflowGraphConstructorError(provider, otherTasks, agentTasks),
			code: graphMismatch,
		},
		"workflow with mismatched provider": {
			err:  workflowGraphConstructorError(otherProvider, tasks, agentTasks),
			code: graphMismatch,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertRepositoryOopsCode(t, tt.err, tt.code)
		})
	}
}

func TestRepositoryMethodsRejectNilEntities(t *testing.T) {
	t.Parallel()

	connection, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	connection.SetMaxOpenConns(1)
	require.NoError(t, Migrate(t.Context(), connection))

	tasks, err := NewTaskRepository(connection)
	require.NoError(t, err)
	documents, err := NewDocumentRepository(connection)
	require.NoError(t, err)
	agentTasks, err := NewAgentTaskRepository(connection)
	require.NoError(t, err)
	workflows, err := NewWorkflowRepository(connection)
	require.NoError(t, err)
	sessions, err := NewSessionRepository(connection)
	require.NoError(t, err)

	tests := nilEntityCases(t, tasks, documents, agentTasks, workflows, sessions)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assertRepositoryOopsCode(t, test.call(), test.code)
		})
	}
}

func nilEntityCases(
	t *testing.T,
	tasks *TaskRepository,
	documents *DocumentRepository,
	agentTasks *AgentTaskRepository,
	workflows *WorkflowRepository,
	sessions *SessionRepository,
) []nilEntityCase {
	t.Helper()

	return []nilEntityCase{
		{name: repositoryTaskName, call: func() error {
			_, createErr := tasks.Create(t.Context(), nil)

			return createErr
		}, code: "nil_task"},
		{
			name: repositoryDocumentName,
			call: func() error { return documents.Put(t.Context(), nil) },
			code: "nil_document",
		},
		{name: repositoryAgentTaskName, call: func() error {
			_, createErr := agentTasks.Create(t.Context(), nil)

			return createErr
		}, code: "nil_agent_task"},
		{name: repositoryWorkflowName, call: func() error {
			_, createErr := workflows.Create(t.Context(), nil)

			return createErr
		}, code: "nil_workflow_run"},
		{name: "message", call: func() error {
			_, appendErr := sessions.AppendMessage(t.Context(), "", nil, nil)

			return appendErr
		}, code: "nil_message"},
		{name: "task claim", call: func() error {
			_, claimErr := tasks.ClaimQueued(t.Context(), nil)

			return claimErr
		}, code: "nil_task_claim"},
		{name: "task finish", call: func() error {
			_, finishErr := tasks.Finish(t.Context(), nil)

			return finishErr
		}, code: "nil_task_finish"},
		{name: "task recovery", call: func() error {
			_, recoveryErr := tasks.RecoverExpired(t.Context(), nil)

			return recoveryErr
		}, code: "nil_task_recovery"},
		{name: "agent task finish", call: func() error {
			_, finishErr := agentTasks.Finish(t.Context(), nil, "{}")

			return finishErr
		}, code: "nil_task_finish"},
		{name: "agent task child session request", call: func() error {
			_, createErr := agentTasks.CreateWithChildSession(t.Context(), new(AgentTaskEntity), nil)

			return createErr
		}, code: "nil_child_session_request"},
		{name: "workflow agent task child session request", call: func() error {
			_, createErr := workflows.CreateAgentTaskWithChildSession(
				t.Context(), "", new(AgentTaskEntity), nil, "", 0,
			)

			return createErr
		}, code: "nil_child_session_request"},
	}
}

func agentTaskGraphConstructorError(provider ksql.Provider, tasks *TaskRepository) error {
	_, err := NewAgentTaskRepositoryWithProvider(provider, tasks)

	return err
}

func toolTaskGraphConstructorError(provider ksql.Provider, tasks *TaskRepository) error {
	_, err := NewToolTaskRepositoryWithProvider(provider, tasks)

	return err
}

func workflowGraphConstructorError(
	provider ksql.Provider,
	tasks *TaskRepository,
	agentTasks *AgentTaskRepository,
) error {
	_, err := NewWorkflowRepositoryWithProvider(provider, tasks, agentTasks)

	return err
}

func sessionConstructorError(connection *sql.DB) error {
	_, err := NewSessionRepository(connection)

	return err
}

func documentConstructorError(connection *sql.DB) error {
	_, err := NewDocumentRepository(connection)

	return err
}

func taskConstructorError(connection *sql.DB) error {
	_, err := NewTaskRepository(connection)

	return err
}

func toolTaskConstructorError(connection *sql.DB) error {
	_, err := NewToolTaskRepository(connection)

	return err
}

func agentTaskConstructorError(connection *sql.DB) error {
	_, err := NewAgentTaskRepository(connection)

	return err
}

func workflowConstructorError(connection *sql.DB) error {
	_, err := NewWorkflowRepository(connection)

	return err
}

func sessionProviderConstructorError(provider ksql.Provider) error {
	_, err := NewSessionRepositoryWithProvider(provider)

	return err
}

func documentProviderConstructorError(provider ksql.Provider) error {
	_, err := NewDocumentRepositoryWithProvider(provider)

	return err
}

func taskProviderConstructorError(provider ksql.Provider) error {
	_, err := NewTaskRepositoryWithProvider(provider)

	return err
}

func toolTaskProviderConstructorError(provider ksql.Provider) error {
	_, err := NewToolTaskRepositoryWithProvider(provider, nil)

	return err
}

func agentTaskProviderConstructorError(provider ksql.Provider) error {
	_, err := NewAgentTaskRepositoryWithProvider(provider, nil)

	return err
}

func workflowProviderConstructorError(provider ksql.Provider) error {
	_, err := NewWorkflowRepositoryWithProvider(provider, nil, nil)

	return err
}

func assertRepositoryOopsCode(t *testing.T, err error, code string) {
	t.Helper()
	require.Error(t, err)
	coded, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, code, coded.Code())
}
