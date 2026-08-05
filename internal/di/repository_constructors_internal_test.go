package di

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

func mustSessionRepository(t *testing.T, connection *sql.DB) *database.SessionRepository {
	t.Helper()

	repository, err := database.NewSessionRepository(connection)
	require.NoError(t, err)

	return repository
}

func mustDocumentRepository(t *testing.T, connection *sql.DB) *database.DocumentRepository {
	t.Helper()

	repository, err := database.NewDocumentRepository(connection)
	require.NoError(t, err)

	return repository
}

func mustTaskRepository(t *testing.T, connection *sql.DB) *database.TaskRepository {
	t.Helper()

	repository, err := database.NewTaskRepository(connection)
	require.NoError(t, err)

	return repository
}

func mustAgentTaskRepository(t *testing.T, connection *sql.DB) *database.AgentTaskRepository {
	t.Helper()

	repository, err := database.NewAgentTaskRepository(connection)
	require.NoError(t, err)

	return repository
}

func mustWorkflowRepository(t *testing.T, connection *sql.DB) *database.WorkflowRepository {
	t.Helper()

	repository, err := database.NewWorkflowRepository(connection)
	require.NoError(t, err)

	return repository
}
