package model_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/auth"
	"github.com/omarluq/librecode/internal/model"
)

func TestRegistryLoadsCustomModelsAndProviderOverrides(t *testing.T) {
	t.Parallel()

	modelsPath := filepath.Join(t.TempDir(), "models.json")
	writeModelFile(t, modelsPath, strings.Join([]string{
		"{",
		"  // comments and trailing commas are accepted",
		"  \"providers\": {",
		"    \"openai\": {",
		"      \"api\" + \"Key\": \"from-config\",",
		"      \"baseUrl\": \"https://example.invalid\",",
		"      \"headers\": {\"X-Test\": \"yes\"},",
		"      \"modelOverrides\": {",
		"        \"gpt-5.4\": {\"name\": \"Overridden GPT\", \"maxTokens\": 99},",
		"      },",
		"      \"models\": [",
		"        {\"id\": \"custom\", \"name\": \"Custom Model\", \"input\": [\"text\", \"image\"]},",
		"      ],",
		"    },",
		"  },",
		"}",
	}, "\n"))

	registry := model.NewRegistry(&model.RegistryOptions{
		ConfigSource: nil,
		Auth:         nil,
		ModelsPath:   modelsPath,
		BuiltIns:     []model.Model{testModel("openai", "gpt-5.4", "GPT")},
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

	storage, err := auth.NewInMemoryStorage(context.Background(), map[string]auth.Credential{
		"auth-provider": testCredential(),
	})
	require.NoError(t, err)
	registry := model.NewRegistry(&model.RegistryOptions{
		ConfigSource: nil,
		Auth:         storage,
		ModelsPath:   "",
		BuiltIns: []model.Model{
			testModel("auth-provider", "claude", "Claude"),
			testModel("noauth-provider", "gpt", "GPT"),
		},
	})

	available := registry.Available()
	require.Len(t, available, 1)
	assert.Equal(t, "auth-provider", available[0].Provider)
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
