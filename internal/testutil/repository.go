package testutil

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

// SessionRepository constructs a session repository or fails the test.
func SessionRepository(tb testing.TB, connection *sql.DB) *database.SessionRepository {
	tb.Helper()

	return repository(tb, connection, database.NewSessionRepository)
}

// DocumentRepository constructs a document repository or fails the test.
func DocumentRepository(tb testing.TB, connection *sql.DB) *database.DocumentRepository {
	tb.Helper()

	return repository(tb, connection, database.NewDocumentRepository)
}

// TaskRepository constructs a task repository or fails the test.
func TaskRepository(tb testing.TB, connection *sql.DB) *database.TaskRepository {
	tb.Helper()

	return repository(tb, connection, database.NewTaskRepository)
}

// AgentTaskRepository constructs an agent-task repository or fails the test.
func AgentTaskRepository(tb testing.TB, connection *sql.DB) *database.AgentTaskRepository {
	tb.Helper()

	return repository(tb, connection, database.NewAgentTaskRepository)
}

// WorkflowRepository constructs a workflow repository or fails the test.
func WorkflowRepository(tb testing.TB, connection *sql.DB) *database.WorkflowRepository {
	tb.Helper()

	return repository(tb, connection, database.NewWorkflowRepository)
}

func repository[T any](
	tb testing.TB,
	connection *sql.DB,
	construct func(*sql.DB) (*T, error),
) *T {
	tb.Helper()

	repository, err := construct(connection)
	require.NoError(tb, err)

	return repository
}
