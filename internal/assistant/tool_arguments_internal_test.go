package assistant

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/extension"
)

func TestApplyToolCallMutationRejectsInvalidJSONArguments(t *testing.T) {
	t.Parallel()

	call := &ToolCallEvent{
		Arguments:     nil,
		ArgumentsJSON: `{"path":"old.txt"}`,
		ID:            "",
		Name:          "",
	}
	mutation := extension.ToolCallMutation{
		Arguments: map[string]any{"invalid": math.Inf(1)},
	}

	err := applyToolCallMutation(call, mutation)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "encode mutated tool arguments")
	assert.JSONEq(t, `{"path":"old.txt"}`, call.ArgumentsJSON)
	assert.Empty(t, call.Arguments)
}

func TestApplyToolCallMutationAppliesEmptyArguments(t *testing.T) {
	t.Parallel()

	call := &ToolCallEvent{
		Arguments:     map[string]any{"path": "old.txt"},
		ArgumentsJSON: `{"path":"old.txt"}`,
		ID:            "",
		Name:          "",
	}
	mutation := extension.ToolCallMutation{
		Arguments: map[string]any{},
	}

	err := applyToolCallMutation(call, mutation)

	require.NoError(t, err)
	assert.Empty(t, call.Arguments)
	assert.JSONEq(t, `{}`, call.ArgumentsJSON)
}

func TestEncodeToolArguments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		arguments map[string]any
		name      string
		wantJSON  string
	}{
		{
			name:      "empty",
			arguments: nil,
			wantJSON:  `{}`,
		},
		{
			name:      "encodes arguments",
			arguments: map[string]any{"path": "README.md", "limit": 3},
			wantJSON:  `{"limit":3,"path":"README.md"}`,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := encodeToolArguments(testCase.arguments)

			require.NoError(t, err)
			assert.JSONEq(t, testCase.wantJSON, got)
		})
	}
}
