package assistant

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
