package assistant

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/testutil"
	"github.com/omarluq/librecode/internal/tool"
)

func TestApplyToolCallMutationAppliesEmptyArguments(t *testing.T) {
	t.Parallel()

	call := &ToolCallEvent{
		ParentCallID: "",
		Sequence:     0,

		Arguments:     testutil.ToolArguments(map[string]any{"path": "old.txt"}),
		ArgumentsJSON: `{"path":"old.txt"}`,
		ID:            "",
		Name:          "",
	}
	mutation := extension.ToolCallMutation{
		Arguments: tool.EmptyArguments(),
		HasArgs:   true,
	}

	err := applyToolCallMutation(call, mutation)

	require.NoError(t, err)
	assert.Empty(t, testutil.ToolArgumentFields(call.Arguments))
	assert.JSONEq(t, `{}`, call.ArgumentsJSON)
}
