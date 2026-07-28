package database_test

import (
	"context"
	"testing"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

const compactionTestHistory = "compaction history"

func TestSessionRepositoryAppendCompactionValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    func(*database.SessionEntity, *database.EntryEntity) *database.AppendCompactionInput
		name     string
		wantCode string
	}{
		{
			name: "nil input",
			input: func(_ *database.SessionEntity, _ *database.EntryEntity) *database.AppendCompactionInput {
				return nil
			},
			wantCode: "append_compaction_nil_input",
		},
		{
			name: "missing operation id",
			input: func(session *database.SessionEntity, parent *database.EntryEntity) *database.AppendCompactionInput {
				return compactionValidationInput(session.ID, parent.ID, "")
			},
			wantCode: "compaction_operation_required",
		},
		{
			name: "missing parent",
			input: func(session *database.SessionEntity, _ *database.EntryEntity) *database.AppendCompactionInput {
				return compactionValidationInput(
					session.ID,
					uuid.Must(uuid.NewV7()).String(),
					uuid.Must(uuid.NewV7()).String(),
				)
			},
			wantCode: "compaction_parent_missing",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			repository := newTestSessionRepository(t)
			session, err := repository.CreateSession(ctx, t.TempDir(), "validation", "")
			require.NoError(t, err)
			parent, err := repository.AppendMessage(ctx, session.ID, nil, &database.MessageEntity{
				Timestamp: time.Now().UTC(),
				Role:      database.RoleUser,
				Content:   compactionTestHistory,
				Provider:  "",
				Model:     "",
			})
			require.NoError(t, err)

			entry, err := repository.AppendCompaction(ctx, test.input(session, parent))

			require.Error(t, err)
			assert.Nil(t, entry)

			oopsErr, ok := oops.AsOops(err)
			require.True(t, ok)
			assert.Equal(t, test.wantCode, oopsErr.Code())
		})
	}
}

func compactionValidationInput(sessionID, parentID, operationID string) *database.AppendCompactionInput {
	return &database.AppendCompactionInput{
		ParentID:         &parentID,
		Details:          nil,
		SessionID:        sessionID,
		Summary:          "summary",
		FirstKeptEntryID: parentID,
		TokensBefore:     10,
		FromHook:         false,
		OperationID:      operationID,
	}
}
