package auth_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/auth"
)

const testStoredKey = "stored-key"

func TestStorageResolvesAuthSourcesWithoutExposingSecrets(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	storage, err := auth.NewInMemoryStorage(ctx, map[string]auth.Credential{
		"openai": {OAuth: nil, Type: auth.CredentialTypeAPIKey, Key: testStoredKey, ExpiresAt: 0},
	})
	require.NoError(t, err)

	apiKey, found := storage.APIKey("openai")
	require.True(t, found)
	assert.Equal(t, testStoredKey, apiKey)
	assert.Equal(t, auth.Status{Source: auth.SourceStored, Label: "", Configured: true}, storage.AuthStatus("openai"))

	storage.SetRuntimeAPIKey("openai", "runtime-key")
	apiKey, found = storage.APIKey("openai")
	require.True(t, found)
	assert.Equal(t, "runtime-key", apiKey)

	storage.RemoveRuntimeAPIKey("openai")
	storage.SetFallbackResolver(func(provider string) (string, bool) {
		return "fallback-" + provider, provider == "custom"
	})
	apiKey, found = storage.APIKey("custom")
	require.True(t, found)
	assert.Equal(t, "fallback-custom", apiKey)
	assert.Equal(t,
		auth.Status{Source: auth.SourceFallback, Label: "custom provider config", Configured: false},
		storage.AuthStatus("custom"),
	)
}

func TestStoragePersistsFileCredentials(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	authPath := filepath.Join(t.TempDir(), "auth.json")
	storage, err := auth.NewStorage(ctx, auth.NewFileBackend(authPath))
	require.NoError(t, err)

	credential := auth.Credential{OAuth: nil, Type: auth.CredentialTypeAPIKey, Key: testStoredKey, ExpiresAt: 0}
	require.NoError(t, storage.Set(ctx, "openai", &credential))
	assert.Equal(t, []string{"openai"}, storage.List())

	reloaded, err := auth.NewStorage(ctx, auth.NewFileBackend(authPath))
	require.NoError(t, err)
	apiKey, found := reloaded.APIKey("openai")
	require.True(t, found)
	assert.Equal(t, testStoredKey, apiKey)

	info, err := os.Stat(authPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	require.NoError(t, reloaded.Remove(ctx, "openai"))
	assert.False(t, reloaded.HasStored("openai"))
}

func TestStoragePersistsMemoryCredentials(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	backend, err := auth.NewMemoryBackend(map[string]auth.Credential{})
	require.NoError(t, err)
	storage, err := auth.NewStorage(ctx, backend)
	require.NoError(t, err)

	credential := auth.Credential{OAuth: nil, Type: auth.CredentialTypeAPIKey, Key: testStoredKey, ExpiresAt: 0}
	require.NoError(t, storage.Set(ctx, "openai", &credential))

	reloaded, err := auth.NewStorage(ctx, backend)
	require.NoError(t, err)
	apiKey, found := reloaded.APIKey("openai")
	require.True(t, found)
	assert.Equal(t, testStoredKey, apiKey)
}
