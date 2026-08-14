package assistant

import (
	"errors"
	"testing"

	"github.com/gofrs/uuid/v5"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/compaction"
	"github.com/omarluq/librecode/internal/extension"
)

const compactionTestOrigin = "test"

func TestRuntimeCompactionOperationIDReturnsGenerationError(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("entropy unavailable")
	runtime := newRuntimeFromDeps(nil)
	runtime.newCompactionUUID = func() (uuid.UUID, error) {
		return uuid.Nil, expectedErr
	}

	operationID, err := runtime.compactionOperationID()

	require.ErrorIs(t, err, expectedErr)
	assert.Empty(t, operationID)

	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "compact_operation_id", oopsErr.Code())
}

func TestCompactionDecisionFromMutation(t *testing.T) {
	t.Parallel()

	plan := &compaction.Plan{
		ValidationRecords:   nil,
		ActiveWorkRecords:   nil,
		SummaryGroups:       nil,
		FirstKeptEntryID:    "",
		Messages:            nil,
		PreviousSummary:     "",
		SplitTurnSummary:    "",
		SummarizedEntryIDs:  nil,
		KeptEntryIDs:        []string{"kept-1", "kept-2"},
		FileOperations:      nil,
		TokensBefore:        0,
		FirstKeptEntryIndex: 0,
	}

	for _, testCase := range compactionDecisionMutationCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			decision, err := compactionDecisionFromMutation(testCase.mutation, plan)
			if testCase.wantNoDecision {
				require.ErrorIs(t, err, errNoCompactionDecision)
				assert.Nil(t, decision)

				return
			}

			if testCase.wantCode != "" {
				require.Error(t, err)

				var oopsErr oops.OopsError
				require.ErrorAs(t, err, &oopsErr)
				assert.Equal(t, testCase.wantCode, oopsErr.Code())
				assert.Nil(t, decision)

				return
			}

			require.NoError(t, err)
			require.NotNil(t, decision)
			assert.Equal(t, "extension summary", decision.Summary)
			assert.Equal(t, "kept-2", decision.FirstKeptEntryID)
			assert.Equal(t, map[string]any{"origin": compactionTestOrigin}, decision.Details)
			assert.True(t, decision.FromHook)
		})
	}
}

type compactionDecisionMutationCase struct {
	mutation       extension.CompactionMutation
	name           string
	wantCode       string
	wantNoDecision bool
}

func compactionDecisionMutationCases() []compactionDecisionMutationCase {
	summary := " extension summary "
	emptySummary := "   "
	firstKept := " kept-2 "
	invalidFirstKept := runtimeSlashMissing

	return []compactionDecisionMutationCase{
		{
			mutation: extension.CompactionMutation{
				Summary:          nil,
				FirstKeptEntryID: nil,
				Details:          nil,
				Cancel:           false,
			},
			name:           "no decision",
			wantCode:       "",
			wantNoDecision: true,
		},
		{
			mutation: extension.CompactionMutation{
				Summary:          &emptySummary,
				FirstKeptEntryID: nil,
				Details:          nil,
				Cancel:           false,
			},
			name:           "empty summary",
			wantCode:       "compact_hook_empty_summary",
			wantNoDecision: false,
		},
		{
			mutation: extension.CompactionMutation{
				Summary:          nil,
				FirstKeptEntryID: &invalidFirstKept,
				Details:          nil,
				Cancel:           false,
			},
			name:           "invalid first kept",
			wantCode:       "compact_hook_invalid_first_kept",
			wantNoDecision: false,
		},
		{
			mutation: extension.CompactionMutation{
				Details:          map[string]any{"origin": compactionTestOrigin},
				Summary:          &summary,
				FirstKeptEntryID: &firstKept,
				Cancel:           false,
			},
			name:           "valid decision",
			wantCode:       "",
			wantNoDecision: false,
		},
	}
}
