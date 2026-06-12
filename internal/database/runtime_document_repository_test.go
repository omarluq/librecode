package database_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

const testDocumentNamespace = "settings"

func TestDocumentRepositoryStoresRuntimeDocuments(t *testing.T) {
	t.Parallel()

	connection := newTestConnection(t)
	repository := database.NewDocumentRepository(connection)
	ctx := context.Background()

	document := database.DocumentEntity{
		UpdatedAt: time.Time{},
		Namespace: testDocumentNamespace,
		Key:       "global",
		ValueJSON: `{"ok":true}`,
	}
	require.NoError(t, repository.Put(ctx, &document))

	loaded, found, err := repository.Get(ctx, testDocumentNamespace, "global")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, document.ValueJSON, loaded.ValueJSON)

	source := database.NewDocumentSource(repository, testDocumentNamespace, "global")
	content, found, err := source.Read()
	require.NoError(t, err)
	require.True(t, found)
	assert.JSONEq(t, document.ValueJSON, string(content))

	require.NoError(t, repository.Delete(ctx, testDocumentNamespace, "global"))
	_, found, err = repository.Get(ctx, testDocumentNamespace, "global")
	require.NoError(t, err)
	assert.False(t, found)
}

func newTestConnection(t *testing.T) *sql.DB {
	t.Helper()

	connection, err := sql.Open(sqliteDriver(), ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, connection.Close())
	})
	connection.SetMaxOpenConns(1)
	require.NoError(t, database.Migrate(context.Background(), connection))

	return connection
}
