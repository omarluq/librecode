package provider

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecuteToolCallsUsesFallbackRegistryCWD(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	_, events := executeToolCalls(
		context.Background(),
		nil,
		cwd,
		[]ToolCall{{
			Arguments:     map[string]any{jsonCommandKey: "pwd"},
			ID:            testCallID,
			Name:          jsonBashToolName,
			ArgumentsJSON: `{"command":"pwd"}`,
			TextFallback:  false,
		}},
		nil,
		nil,
		nil,
	)

	require.Len(t, events, 1)
	assert.Contains(t, events[0].Result, cwd)
}
