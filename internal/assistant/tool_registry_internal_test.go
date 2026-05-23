package assistant

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/tool"
)

type nilToolProvider struct{}

func (*nilToolProvider) Tools() []extension.Tool {
	panic("typed nil provider Tools called")
}

func (*nilToolProvider) ExecuteTool(context.Context, string, map[string]any) (extension.ToolResult, error) {
	panic("typed nil provider ExecuteTool called")
}

func TestNewToolRegistryHandlesNilProviders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		provider toolProvider
		name     string
	}{
		{name: "nil interface", provider: nil},
		{name: "typed nil", provider: (*nilToolProvider)(nil)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registry, err := newToolRegistry(t.TempDir(), tt.provider)

			require.NoError(t, err)
			assert.NotNil(t, registry)
			assert.Equal(t, len(tool.AllDefinitions()), len(registry.Definitions()))
		})
	}
}

func TestExecuteToolCallsUsesFallbackRegistryCWD(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	_, events := executeToolCalls(
		context.Background(),
		nil,
		cwd,
		[]toolCall{{
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
