package assistant

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/model"
)

func TestDelegationModelSelection(t *testing.T) {
	t.Parallel()

	var chat model.Model

	chat.Provider, chat.ID = "chat", "large"

	var cheap model.Model

	cheap.Provider, cheap.ID = "cheap", "small"

	var cfg config.Config

	cfg.Assistant.Provider, cfg.Assistant.Model = chat.Provider, chat.ID
	cfg.Delegation.Provider, cfg.Delegation.Model = cheap.Provider, cheap.ID
	cfg.Delegation.ThinkingLevel = "off"

	var registryOptions model.RegistryOptions

	registryOptions.BuiltIns = []model.Model{chat, cheap}

	var runtime Runtime

	runtime.cfg = &cfg
	runtime.models = model.NewRegistry(&registryOptions)

	runtime.profile = ExecutionProfile{Kind: ExecutionAgentTask}

	got, err := runtime.selectedModel()
	require.NoError(t, err)
	assert.Equal(t, cheap.Provider, got.Provider)
	assert.Equal(t, cheap.ID, got.ID)

	runtime.profile = topLevelExecutionProfile()

	got, err = runtime.selectedModel()
	require.NoError(t, err)
	assert.Equal(t, chat.Provider, got.Provider)
	assert.Equal(t, chat.ID, got.ID)

	runtime.profile = ExecutionProfile{Kind: ExecutionAgentTask, Provider: "override", Model: "custom"}

	got, err = runtime.selectedModel()
	require.NoError(t, err)
	assert.Equal(t, "override", got.Provider)
	assert.Equal(t, "custom", got.ID)
}

func TestDelegationThinkingLevel(t *testing.T) {
	t.Parallel()

	var cfg config.Config

	cfg.Assistant.ThinkingLevel = "high"
	cfg.Delegation.ThinkingLevel = "off"

	var runtime Runtime

	runtime.cfg = &cfg

	runtime.profile = ExecutionProfile{Kind: ExecutionAgentTask}
	assert.Equal(t, "off", runtime.thinkingLevel())

	runtime.profile = topLevelExecutionProfile()
	assert.Equal(t, "high", runtime.thinkingLevel())

	runtime.profile = ExecutionProfile{Kind: ExecutionAgentTask, ThinkingLevel: "low"}
	assert.Equal(t, "low", runtime.thinkingLevel())
}
