package database

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ksqlite "github.com/vingarcia/ksql/adapters/modernc-ksqlite"

	_ "modernc.org/sqlite" // register the SQLite driver used by sql.Open in this test.
)

func TestInsertEntryIgnoringConflictReturnsNonUniqueConstraintFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	connection, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	connection.SetMaxOpenConns(1)
	require.NoError(t, Migrate(ctx, connection))
	_, err = connection.ExecContext(ctx, `CREATE TABLE compaction_insert_audit (value TEXT NOT NULL)`)
	require.NoError(t, err)
	_, err = connection.ExecContext(ctx, `CREATE TRIGGER reject_compaction_insert
AFTER INSERT ON session_entries
WHEN NEW.entry_type = 'compaction'
BEGIN
    INSERT INTO compaction_insert_audit(value) VALUES (NULL);
END`)
	require.NoError(t, err)

	provider, err := ksqlite.NewFromSQLDB(connection)
	require.NoError(t, err)

	repository, err := NewSessionRepositoryWithProvider(provider)
	require.NoError(t, err)
	entry := validConstraintTestCompactionEntry(t)

	inserted, err := repository.insertEntryIgnoringConflictTx(
		ctx,
		provider,
		entry,
		uuid.Must(uuid.NewV7()).String(),
		true,
	)

	require.Error(t, err)
	assert.False(t, inserted)
	assert.ErrorContains(t, err, "NOT NULL constraint failed")
}

func validConstraintTestCompactionEntry(t *testing.T) *EntryEntity {
	t.Helper()

	return &EntryEntity{
		CreatedAt: time.Now().UTC(),
		ParentID:  nil,
		Message: MessageEntity{
			Timestamp: time.Now().UTC(),
			Role:      RoleAssistant,
			Content:   "",
			Provider:  "",
			Model:     "", Parts: nil,
		},
		Summary:                    "summary",
		ToolStatus:                 "",
		Type:                       EntryTypeCompaction,
		CustomType:                 "",
		DataJSON:                   "{}",
		ID:                         uuid.Must(uuid.NewV7()).String(),
		ToolName:                   "",
		SessionID:                  uuid.Must(uuid.NewV7()).String(),
		ToolArgsJSON:               "",
		BranchFromEntryID:          "",
		CompactionFirstKeptEntryID: "",
		CompactionTokensBefore:     1,
		TokenEstimate:              1,
		Display:                    true,
		ModelFacing:                true,
	}
}

func TestCollectSQLRowsReturnsConvertedRows(t *testing.T) {
	t.Parallel()

	rows, err := collectSQLRows([]string{"alpha", "beta"}, func(row *string) (*int, error) {
		length := len(*row)

		return &length, nil
	})

	require.NoError(t, err)
	assert.Equal(t, []int{5, 4}, rows)
}

func TestCollectSQLRowsReturnsConversionError(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("bad row")
	rows, err := collectSQLRows([]string{"alpha"}, func(*string) (*int, error) {
		return nil, expectedErr
	})

	require.ErrorIs(t, err, expectedErr)
	assert.Nil(t, rows)
}

const (
	testRowSessionID = "session-id"
	testRowCreatedAt = "2026-01-01T00:00:00Z"
)

func TestRowConvertersReturnTimestampErrors(t *testing.T) {
	t.Parallel()

	const invalidTimestamp = "not-time"

	tests := []struct {
		run  func() error
		name string
	}{
		{
			name: "session created_at",
			run: func() error {
				row := validSessionRow()
				row.CreatedAt = invalidTimestamp
				_, err := sessionFromRow(&row)

				return err
			},
		},
		{
			name: "session updated_at",
			run: func() error {
				row := validSessionRow()
				row.UpdatedAt = invalidTimestamp
				_, err := sessionFromRow(&row)

				return err
			},
		},
		{
			name: "entry created_at",
			run: func() error {
				row := validEntryRow()
				row.CreatedAt = invalidTimestamp
				_, err := entryFromRow(&row)

				return err
			},
		},
		{
			name: "session_message created_at",
			run: func() error {
				row := validSessionMessageRow()
				row.CreatedAt = invalidTimestamp
				_, err := sessionMessageFromRow(&row, nil)

				return err
			},
		},
		{
			name: "document updated_at",
			run: func() error {
				row := validDocumentRow()
				row.UpdatedAt = invalidTimestamp
				_, err := documentFromRow(&row)

				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := test.run()
			require.Error(t, err)
			assert.ErrorContains(t, err, "parse timestamp")
		})
	}
}

func validSessionRow() sessionRow {
	const testCWD = "/work"

	return sessionRow{
		ID:            testRowSessionID,
		CWD:           testCWD,
		Name:          repositorySessionName,
		ParentSession: nil,
		CreatedAt:     testRowCreatedAt,
		UpdatedAt:     "2026-01-01T00:00:01Z",
	}
}

func validEntryRow() entryRow {
	return entryRow{
		ParentID:                   nil,
		ID:                         "entry-id",
		SessionID:                  testRowSessionID,
		EntryType:                  string(EntryTypeMessage),
		CustomType:                 "",
		DataJSON:                   "{}",
		Summary:                    "",
		CreatedAt:                  testRowCreatedAt,
		ToolName:                   "",
		ToolStatus:                 "",
		ToolArgsJSON:               "",
		CompactionFirstKeptEntryID: "",
		BranchFromEntryID:          "",
		TokenEstimate:              1,
		ModelFacing:                1,
		Display:                    1,
		CompactionTokensBefore:     0,
	}
}

func validSessionMessageRow() sessionMessageRow {
	return sessionMessageRow{
		SessionID:  testRowSessionID,
		EntryID:    "entry-id",
		CustomType: "",
		Role:       string(RoleUser),
		Provider:   "",
		Model:      "",
		CreatedAt:  testRowCreatedAt,
	}
}

func validDocumentRow() documentRow {
	return documentRow{
		Namespace: "settings",
		Key:       "global",
		ValueJSON: "{}",
		UpdatedAt: testRowCreatedAt,
	}
}
