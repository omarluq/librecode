package database

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
				_, err := sessionMessageFromRow(&row)

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
	return sessionRow{
		ID:            testRowSessionID,
		CWD:           "/work",
		Name:          "session",
		ParentSession: "",
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
		Role:                       string(RoleUser),
		Content:                    "hello",
		Provider:                   "",
		Model:                      "",
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
		ID:        "message-id",
		SessionID: testRowSessionID,
		EntryID:   "entry-id",
		Sender:    string(RoleUser),
		Role:      string(RoleUser),
		Content:   "hello",
		Provider:  "",
		Model:     "",
		CreatedAt: testRowCreatedAt,
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
