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

	chat := model.Model{Provider: "chat", ID: "large"}
	cheap := model.Model{Provider: "cheap", ID: "small"}

	newRuntime := func(t *testing.T) *Runtime {
		t.Helper()
		var cfg config.Config
		cfg.Assistant.Provider, cfg.Assistant.Model = chat.Provider, chat.ID
		cfg.Delegation.Provider, cfg.Delegation.Model = cheap.Provider, cheap.ID
		cfg.Delegation.ThinkingLevel = "off"

		var registryOptions model.RegistryOptions
		registryOptions.BuiltIns = []model.Model{chat, cheap}

		runtime := &Runtime{cfg: &cfg}
		runtime.models = model.NewRegistry(&registryOptions)
		return runtime
	}

	tests := []struct {
		name         string
		profile      ExecutionProfile
		wantProvider string
		wantModel    string
	}{
		{
			name:         "agent task uses delegation model",
			profile:      ExecutionProfile{Kind: ExecutionAgentTask},
			wantProvider: cheap.Provider,
			wantModel:    cheap.ID,
		},
		{
			name:         "top level uses assistant model",
			profile:      topLevelExecutionProfile(),
			wantProvider: chat.Provider,
			wantModel:    chat.ID,
		},
		{
			name:         "profile override wins",
			profile:      ExecutionProfile{Kind: ExecutionAgentTask, Provider: "override", Model: "custom"},
			wantProvider: "override",
			wantModel:    "custom",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			runtime := newRuntime(t)
			runtime.profile = testCase.profile

			got, err := runtime.selectedModel()
			require.NoError(t, err)
			assert.Equal(t, testCase.wantProvider, got.Provider)
			assert.Equal(t, testCase.wantModel, got.ID)
		})
	}
}

func TestDelegationThinkingLevel(t *testing.T) {
	t.Parallel()

	newRuntime := func(t *testing.T) *Runtime {
		t.Helper()
		var cfg config.Config
		cfg.Assistant.ThinkingLevel = "high"
		cfg.Delegation.ThinkingLevel = "off"
		return &Runtime{cfg: &cfg}
	}

	tests := []struct {
		name      string
		profile   ExecutionProfile
		wantLevel string
	}{
		{
			name:      "agent task uses delegation thinking level",
			profile:   ExecutionProfile{Kind: ExecutionAgentTask},
			wantLevel: "off",
		},
		{
			name:      "top level uses assistant thinking level",
			profile:   topLevelExecutionProfile(),
			wantLevel: "high",
		},
		{
			name:      "profile override wins",
			profile:   ExecutionProfile{Kind: ExecutionAgentTask, ThinkingLevel: "low"},
			wantLevel: "low",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			runtime := newRuntime(t)
			runtime.profile = testCase.profile
			assert.Equal(t, testCase.wantLevel, runtime.thinkingLevel())
		})
	}
}
