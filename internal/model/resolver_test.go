package model_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/auth"
	"github.com/omarluq/librecode/internal/model"
)

func TestParseModelPatternHandlesThinkingSuffixes(t *testing.T) {
	t.Parallel()

	models := []model.Model{
		testModel("anthropic", "claude-sonnet-20250101", "Claude Sonnet dated"),
		testModel("anthropic", "claude-sonnet", "Claude Sonnet alias"),
	}

	parsed := model.ParseModelPattern("sonnet:high", models, true)
	require.NotNil(t, parsed.Model)
	require.NotNil(t, parsed.ThinkingLevel)
	assert.Equal(t, "claude-sonnet", parsed.Model.ID)
	assert.Equal(t, model.ThinkingHigh, *parsed.ThinkingLevel)

	parsed = model.ParseModelPattern("sonnet:nope", models, true)
	require.NotNil(t, parsed.Model)
	assert.Nil(t, parsed.ThinkingLevel)
	assert.Contains(t, parsed.Warning, "Invalid thinking level")
}

func TestResolveCLIModelInfersProviderAndRejectsUnknownProvider(t *testing.T) {
	t.Parallel()

	registry := model.NewRegistry(&model.RegistryOptions{
		ConfigSource: nil,
		Auth:         nil,
		ModelsPath:   "",
		BuiltIns: []model.Model{
			testModel("openai", "gpt-5.4", "GPT"),
			testModel("zai", "glm-5.1", "GLM"),
		},
	})

	resolved := model.ResolveCLIModel(model.ResolveCLIModelOptions{
		Registry:    registry,
		CLIProvider: "",
		CLIModel:    "zai/glm",
	})
	require.Empty(t, resolved.Error)
	require.NotNil(t, resolved.Model)
	assert.Equal(t, "zai", resolved.Model.Provider)

	resolved = model.ResolveCLIModel(model.ResolveCLIModelOptions{
		Registry:    registry,
		CLIProvider: "missing",
		CLIModel:    "gpt",
	})
	assert.Contains(t, resolved.Error, "Unknown provider")
}

func TestResolveModelScopeSupportsGlobsAndDeduplicates(t *testing.T) {
	t.Parallel()

	storage, err := auth.NewInMemoryStorage(context.Background(), map[string]auth.Credential{
		"openai": {OAuth: nil, Type: auth.CredentialTypeAPIKey, Key: "stored-key", ExpiresAt: 0},
	})
	require.NoError(t, err)
	registry := model.NewRegistry(&model.RegistryOptions{
		ConfigSource: nil,
		Auth:         storage,
		ModelsPath:   "",
		BuiltIns: []model.Model{
			testModel("openai", "gpt-5.4", "GPT"),
			testModel("openai", "gpt-5.4-20250101", "GPT dated"),
			testModel("anthropic", "claude-opus", "Claude"),
		},
	})

	scopedModels, warnings := model.ResolveModelScope([]string{"openai/gpt*:low", "gpt-5.4"}, registry)
	require.Empty(t, warnings)
	require.Len(t, scopedModels, 2)
	require.NotNil(t, scopedModels[0].ThinkingLevel)
	assert.Equal(t, model.ThinkingLow, *scopedModels[0].ThinkingLevel)
}
