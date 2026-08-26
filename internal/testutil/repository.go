package testutil

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

// Repositories constructs a shared repository graph or fails the test.
func Repositories(tb testing.TB, connection *sql.DB) *database.Repositories {
	tb.Helper()

	repositories, err := database.NewRepositories(connection)
	require.NoError(tb, err)

	return repositories
}

// SessionRepository constructs a session repository or fails the test.
func SessionRepository(tb testing.TB, connection *sql.DB) *database.SessionRepository {
	tb.Helper()

	return Repositories(tb, connection).Sessions
}

// DocumentRepository constructs a document repository or fails the test.
func DocumentRepository(tb testing.TB, connection *sql.DB) *database.DocumentRepository {
	tb.Helper()

	return Repositories(tb, connection).Documents
}

// TaskRepository constructs a task repository or fails the test.
func TaskRepository(tb testing.TB, connection *sql.DB) *database.TaskRepository {
	tb.Helper()

	return Repositories(tb, connection).Tasks
}

// AgentTaskRepository constructs an agent-task repository or fails the test.
func AgentTaskRepository(tb testing.TB, connection *sql.DB) *database.AgentTaskRepository {
	tb.Helper()

	return Repositories(tb, connection).AgentTasks
}

// ToolTaskRepository constructs a tool-task repository or fails the test.
func ToolTaskRepository(tb testing.TB, connection *sql.DB) *database.ToolTaskRepository {
	tb.Helper()

	return Repositories(tb, connection).ToolTasks
}

// WorkflowRepository constructs a workflow repository or fails the test.
func WorkflowRepository(tb testing.TB, connection *sql.DB) *database.WorkflowRepository {
	tb.Helper()

	return Repositories(tb, connection).Workflows
}
