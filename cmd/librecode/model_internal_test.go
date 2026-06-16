package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/auth"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/testutil"
)

func TestPrintModelsIncludesCapabilityColumns(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer

	err := printModels(&output, []model.Model{
		{
			ThinkingLevelMap: nil,
			Headers:          nil,
			Compat:           nil,
			Provider:         "openai",
			ID:               "gpt-5.4",
			Name:             "GPT-5.4",
			API:              "openai-responses",
			BaseURL:          "https://api.openai.com/v1",
			Input:            []model.InputMode{model.InputText, model.InputImage},
			Cost:             model.Cost{Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0},
			ContextWindow:    272000,
			MaxTokens:        128000,
			Reasoning:        true,
		},
	})
	require.NoError(t, err)

	assert.Contains(t, output.String(), "provider")
	assert.Contains(t, output.String(), "context")
	assert.Contains(t, output.String(), "272K")
	assert.Contains(t, output.String(), "128K")
	assert.Contains(t, output.String(), "yes")
}

func TestListedModelsDefaultsToAuthorizedProviders(t *testing.T) {
	t.Parallel()

	storage := testutil.NewAuthStorage(t, map[string]auth.Credential{
		"anthropic-claude": testCLIAuthCredential(),
	})

	registry := model.NewRegistry(&model.RegistryOptions{
		ConfigReader: nil,
		Auth:         storage,
		ModelsPath:   "",
		BuiltIns: []model.Model{
			testCLIModel("anthropic", "claude-opus-4-7", "Claude API"),
			testCLIModel("anthropic-claude", "claude-opus-4-7", "Claude OAuth"),
		},
		Discovery: disabledCLIDiscovery(),
	})

	models := listedModels(registry, false)
	require.Len(t, models, 1)
	assert.Equal(t, "anthropic-claude", models[0].Provider)

	allModels := listedModels(registry, true)
	require.Len(t, allModels, 2)
}

func TestFilterModelListMatchesProviderIDAndName(t *testing.T) {
	t.Parallel()

	models := []model.Model{
		testCLIModel("openai", "gpt-5.4", "GPT-5.4"),
		testCLIModel("anthropic-claude", "claude-opus-4-7", "Claude Opus OAuth"),
	}

	assert.Equal(t, []model.Model{models[1]}, filterModelList(models, "oauth"))
	assert.Equal(t, []model.Model{models[0]}, filterModelList(models, "openai"))
	assert.Equal(t, models, filterModelList(models, "  "))
}

func TestModelListFormattingHelpers(t *testing.T) {
	t.Parallel()

	rows := []modelListRow{{
		Provider:  "long-provider",
		Model:     "m",
		Context:   "1M",
		MaxOutput: "128K",
		Reasoning: "yes",
		Images:    "no",
	}}
	widths := computeModelListWidths(rows)
	assert.Equal(t, len("long-provider"), widths.Provider)
	assert.Equal(t, len("max-out"), widths.MaxOutput)

	assert.Equal(t, "999", formatTokenCount(999))
	assert.Equal(t, "1K", formatTokenCount(1000))
	assert.Equal(t, "1.5K", formatTokenCount(1500))
	assert.Equal(t, "1M", formatTokenCount(1_000_000))
	assert.Equal(t, "1.5M", formatTokenCount(1_500_000))
	assert.Equal(t, "yes", yesNo(true))
	assert.Equal(t, "no", yesNo(false))

	imageModel := testCLIModel("openai", "vision", "Vision")
	imageModel.Input = []model.InputMode{model.InputText, model.InputImage}
	textModel := testCLIModel("openai", "text", "Text")

	assert.True(t, modelSupportsImage(&imageModel))
	assert.False(t, modelSupportsImage(&textModel))
}

func testCLIAuthCredential() auth.Credential {
	return auth.Credential{
		OAuth:     nil,
		Type:      auth.CredentialTypeAPIKey,
		Key:       "stored-key",
		Access:    "",
		Refresh:   "",
		AccountID: "",
		Expires:   0,
		ExpiresAt: 0,
	}
}

func disabledCLIDiscovery() model.DiscoveryOptions {
	return model.DiscoveryOptions{
		Client:       nil,
		CachePath:    "",
		SourceURL:    "",
		CacheTTL:     0,
		FetchTimeout: 0,
		Enabled:      false,
	}
}

func testCLIModel(provider, modelID, name string) model.Model {
	return model.Model{
		ThinkingLevelMap: nil,
		Headers:          nil,
		Compat:           nil,
		Provider:         provider,
		ID:               modelID,
		Name:             name,
		API:              "",
		BaseURL:          "",
		Input:            []model.InputMode{model.InputText},
		Cost:             model.Cost{Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0},
		ContextWindow:    0,
		MaxTokens:        0,
		Reasoning:        false,
	}
}
