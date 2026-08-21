package assistant

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/agent"
	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/model"
)

const (
	delegationCustomModel = "custom"
	delegationLargeModel  = "large"
	delegationSmallModel  = "small"
)

func delegationChatModel() model.Model {
	return model.Model{
		ThinkingLevelMap: nil,
		Headers:          nil,
		Compat:           nil,
		Provider:         "chat",
		ID:               delegationLargeModel,
		Name:             delegationLargeModel,
		API:              "",
		BaseURL:          "",
		Input:            nil,
		Cost:             model.Cost{Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0},
		ContextWindow:    0,
		MaxTokens:        0,
		Reasoning:        false,
	}
}

func delegationCheapModel() model.Model {
	return model.Model{
		ThinkingLevelMap: nil,
		Headers:          nil,
		Compat:           nil,
		Provider:         "cheap",
		ID:               delegationSmallModel,
		Name:             delegationSmallModel,
		API:              "",
		BaseURL:          "",
		Input:            nil,
		Cost:             model.Cost{Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0},
		ContextWindow:    0,
		MaxTokens:        0,
		Reasoning:        false,
	}
}

func delegationEmptyAgentProfile() ExecutionProfile {
	return ExecutionProfile{
		Kind:             ExecutionAgentTask,
		AgentName:        "",
		SystemPrompt:     "",
		Provider:         "",
		Model:            "",
		ThinkingLevel:    "",
		PermissionMode:   agent.PermissionAllow,
		Tools:            nil,
		EnableSkills:     false,
		EnableExtensions: false,
		MaxTurns:         0,
		Depth:            0,
	}
}

func newDelegationModelSelectionRuntime(t *testing.T, chat, cheap *model.Model) *Runtime {
	t.Helper()

	var cfg config.Config

	cfg.Assistant.Provider, cfg.Assistant.Model = chat.Provider, chat.ID
	cfg.Delegation.Provider, cfg.Delegation.Model = cheap.Provider, cheap.ID
	cfg.Delegation.ThinkingLevel = thinkingOff

	var registryOptions model.RegistryOptions

	registryOptions.BuiltIns = []model.Model{*chat, *cheap}

	registry := model.NewRegistry(&registryOptions)

	return NewRuntimeForTest(func(opts *RuntimeTestOptions) {
		opts.Config = &cfg
		opts.Models = registry
	})
}

func TestDelegationModelSelection(t *testing.T) {
	t.Parallel()

	chat := delegationChatModel()
	cheap := delegationCheapModel()

	tests := []struct {
		name         string
		wantProvider string
		wantModel    string
		profile      ExecutionProfile
	}{
		{
			name:         "agent task uses delegation model",
			wantProvider: cheap.Provider,
			wantModel:    cheap.ID,
			profile:      delegationEmptyAgentProfile(),
		},
		{
			name:         "top level uses assistant model",
			wantProvider: chat.Provider,
			wantModel:    chat.ID,
			profile:      topLevelExecutionProfile(),
		},
		{
			name:         "profile override wins",
			wantProvider: "override",
			wantModel:    delegationCustomModel,
			profile: ExecutionProfile{
				Kind:             ExecutionAgentTask,
				AgentName:        "",
				SystemPrompt:     "",
				Provider:         "override",
				Model:            delegationCustomModel,
				ThinkingLevel:    "",
				PermissionMode:   agent.PermissionAllow,
				Tools:            nil,
				EnableSkills:     false,
				EnableExtensions: false,
				MaxTurns:         0,
				Depth:            0,
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			runtime := newDelegationModelSelectionRuntime(t, &chat, &cheap)
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

		cfg.Assistant.ThinkingLevel = adapterThinkingLevel
		cfg.Delegation.ThinkingLevel = thinkingOff

		return NewRuntimeForTest(func(opts *RuntimeTestOptions) {
			opts.Config = &cfg
		})
	}

	tests := []struct {
		name      string
		wantLevel string
		profile   ExecutionProfile
	}{
		{
			name:      "agent task uses delegation thinking level",
			wantLevel: thinkingOff,
			profile:   delegationEmptyAgentProfile(),
		},
		{
			name:      "top level uses assistant thinking level",
			wantLevel: adapterThinkingLevel,
			profile:   topLevelExecutionProfile(),
		},
		{
			name:      "profile override wins",
			wantLevel: "low",
			profile: ExecutionProfile{
				Kind:             ExecutionAgentTask,
				AgentName:        "",
				SystemPrompt:     "",
				Provider:         "",
				Model:            "",
				ThinkingLevel:    "low",
				PermissionMode:   agent.PermissionAllow,
				Tools:            nil,
				EnableSkills:     false,
				EnableExtensions: false,
				MaxTurns:         0,
				Depth:            0,
			},
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
