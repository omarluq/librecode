package database

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	// Register the SQLite driver used by sql.Open in this test.
	_ "modernc.org/sqlite"
)

func TestMigrationProviderLogsStatementsOnlyAtDebugLevel(t *testing.T) {
	t.Parallel()

	migrationRoot := fstest.MapFS{
		"00001_create_test_table.sql": &fstest.MapFile{Data: []byte(`-- +goose Up
CREATE TABLE example (id INTEGER PRIMARY KEY);
-- +goose Down
DROP TABLE example;
`)},
	}

	tests := []struct {
		name     string
		level    slog.Level
		wantLogs bool
	}{
		{name: "info", level: slog.LevelInfo, wantLogs: false},
		{name: "debug", level: slog.LevelDebug, wantLogs: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var output bytes.Buffer

			logger := slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: test.level}))
			connection, err := sql.Open("sqlite", ":memory:")
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, connection.Close()) })

			provider, err := newMigrationProvider(connection, migrationRoot, logger)
			require.NoError(t, err)
			_, err = provider.Up(context.Background())
			require.NoError(t, err)

			assert.Equal(t, test.wantLogs, bytes.Contains(output.Bytes(), []byte(`"logger":"goose"`)))
		})
	}
}
