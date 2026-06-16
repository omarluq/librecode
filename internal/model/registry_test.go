package model_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/auth"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/testutil"
)

func TestRegistryLoadsCustomModelsAndProviderOverrides(t *testing.T) {
	t.Parallel()

	modelsPath := filepath.Join(t.TempDir(), "models.json")
	writeModelFile(t, modelsPath, strings.Join([]string{
		"{",
		"  // comments and trailing commas are accepted",
		"  \"providers\": {",
		"    \"openai\": {",
		"      \"api_key\": \"from-config\",",
		"      \"base_url\": \"https://example.invalid\",",
		"      \"headers\": {\"X-Test\": \"yes\"},",
		"      \"model_overrides\": {",
		"        \"gpt-5.4\": {\"name\": \"Overridden GPT\", \"max_tokens\": 99},",
		"      },",
		"      \"models\": [",
		"        {\"id\": \"custom\", \"name\": \"Custom Model\", \"input\": [\"text\", \"image\"]},",
		"      ],",
		"    },",
		"  },",
		"}",
	}, "\n"))

	registry := model.NewRegistry(&model.RegistryOptions{
		ConfigReader: nil,
		Auth:         nil,
		ModelsPath:   modelsPath,
		BuiltIns:     []model.Model{testModel("openai", "gpt-5.4", "GPT")},
		Discovery:    disabledDiscovery(),
	})
	require.NoError(t, registry.Error())

	allModels := registry.All()
	require.Len(t, allModels, 2)
	assert.Equal(t, "Overridden GPT", allModels[0].Name)
	assert.Equal(t, 99, allModels[0].MaxTokens)
	assert.Equal(t, "https://example.invalid", allModels[0].BaseURL)
	assert.Equal(t, "custom", allModels[1].ID)
	assert.Equal(t, []model.InputMode{model.InputText, model.InputImage}, allModels[1].Input)

	requestAuth := registry.RequestAuth("openai")
	assert.True(t, requestAuth.OK)
	assert.Equal(t, "from-config", requestAuth.APIKey)
	assert.Equal(t, map[string]string{"X-Test": "yes"}, requestAuth.Headers)
}

func TestRegistryAvailableUsesAuthStorage(t *testing.T) {
	t.Parallel()

	assertRegistryAvailableProvider(t, "auth-provider", []model.Model{
		testModel("auth-provider", "claude", "Claude"),
		testModel("noauth-provider", "gpt", "GPT"),
	})
}

func TestRegistryAvailableIncludesOAuthProviderDiscoveredModels(t *testing.T) {
	t.Parallel()

	assertRegistryAvailableProvider(t, "anthropic-claude", []model.Model{
		testModel("anthropic", "claude-opus-4-7", "Claude Opus API"),
		testModel("anthropic-claude", "claude-opus-4-7", "Claude Opus OAuth"),
	})
}

func assertRegistryAvailableProvider(t *testing.T, expectedProvider string, models []model.Model) {
	t.Helper()

	storage := testutil.NewAuthStorage(t, map[string]auth.Credential{
		expectedProvider: testCredential(),
	})

	registry := model.NewRegistry(&model.RegistryOptions{
		ConfigReader: nil,
		Auth:         storage,
		ModelsPath:   "",
		BuiltIns:     models,
		Discovery:    disabledDiscovery(),
	})

	available := registry.Available()
	require.Len(t, available, 1)
	assert.Equal(t, expectedProvider, available[0].Provider)
}

func testCredential() auth.Credential {
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

func writeModelFile(t *testing.T, path, content string) {
	t.Helper()

	content = strings.ReplaceAll(content, "\"api\" + \"Key\"", "\"apiKey\"")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func testModel(provider, modelID, name string) model.Model {
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
