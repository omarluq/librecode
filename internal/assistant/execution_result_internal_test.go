package assistant

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecutionResultKindsAreStable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind ExecutionResultKind
		want string
	}{
		{ExecutionResultCompleted, "completed"},
		{ExecutionResultAccepted, "accepted"},
		{ExecutionResultFailed, "failed"},
		{ExecutionResultCanceled, "canceled"},
		{ExecutionResultTimedOut, "timed_out"},
		{ExecutionResultRejected, "rejected"},
	}

	for _, test := range tests {
		t.Run(test.want, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.want, string(test.kind))
		})
	}
}

func TestExecutionResultDetailsAddsIndependentDiscriminators(t *testing.T) {
	t.Parallel()

	base := map[string]any{"result_value": 42}
	details := executionResultDetails(base, MVMExecutionProfileTurn, ExecutionResultCompleted)

	assert.Equal(t, base, details)
	assert.Equal(t, ExecutionResultCompleted, details[executionResultKindKey])
	assert.Equal(t, executionIdentityMVM, details[executionIdentityKey])
	assert.Equal(t, MVMExecutionProfileTurn, details[executionProfileKey])
	assert.Equal(t, 42, details[executeResultValueKey])
}

func TestExecutionResultEnvelopeOptionalFieldsAreAdditive(t *testing.T) {
	t.Parallel()

	envelope := ExecutionResultEnvelope{
		PartialValue:   map[string]any{"done": 1},
		ResultValue:    nil,
		ToolDetails:    nil,
		Truncation:     nil,
		Usage:          map[string]any{"tokens": 3},
		RunID:          "",
		Stderr:         "",
		ResultKind:     ExecutionResultFailed,
		WorkflowTaskID: "",
		Kind:           "",
		Name:           "",
		State:          "",
		Stdout:         "",
		Profile:        MVMExecutionProfileDurable,
		Execution:      executionIdentityMVM,
		Content:        nil,
		Artifacts:      []map[string]any{{"id": "artifact-1"}},
		Warnings:       []string{"bounded"},
	}

	encoded, _, _, _, err := encodeProviderVisibleExecutionValue(envelope)
	require.NoError(t, err)
	assert.Contains(t, encoded, `"artifacts"`)
	assert.Contains(t, encoded, `"usage"`)
	assert.Contains(t, encoded, `"warnings"`)
	assert.Contains(t, encoded, `"partial_value"`)
}
