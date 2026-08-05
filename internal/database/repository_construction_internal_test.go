package database

import (
	"database/sql"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vingarcia/ksql"
)

const (
	repositorySessionName   = "session"
	repositoryDocumentName  = "document"
	repositoryTaskName      = "task"
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

func agentTaskProviderConstructorError(provider ksql.Provider) error {
	_, err := NewAgentTaskRepositoryWithProvider(provider)

	return err
}

func workflowProviderConstructorError(provider ksql.Provider) error {
	_, err := NewWorkflowRepositoryWithProvider(provider)

	return err
}

func assertRepositoryOopsCode(t *testing.T, err error, code string) {
	t.Helper()
	require.Error(t, err)
	coded, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, code, coded.Code())
}
