package provider

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/model"
)

func TestExecuteToolCallsUsesFallbackRegistryCWD(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	request := &CompletionRequest{
		OnEvent:           nil,
		OnProviderObserve: nil,
		OnProviderRequest: nil,
		OnToolCall:        nil,
		OnToolResult:      nil,
		ToolRegistry:      nil,
		ExecuteTools:      nil,
		SessionID:         "",
		SystemPrompt:      "",
		ThinkingLevel:     "",
		CWD:               cwd,
		Auth:              emptyRequestAuth(),
		Messages:          nil,
		Usage:             model.EmptyTokenUsage(),
		Model:             emptyModel(),
		ProviderAttempt:   0,
		DisableTools:      false,
	}
	_, events, err := executeToolCalls(context.Background(), request, []ToolCall{{
		Arguments:     map[string]any{jsonCommandKey: "pwd"},
		ID:            testCallID,
		Name:          jsonBashToolName,
		ArgumentsJSON: `{"command":"pwd"}`,
		TextFallback:  false,
	}})
	require.NoError(t, err)

	require.Len(t, events, 1)
	assert.Contains(t, events[0].Result, cwd)
}
