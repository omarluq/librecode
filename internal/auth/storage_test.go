package auth_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/auth"
	"github.com/omarluq/librecode/internal/testutil"
)

const (
	testStoredKey          = "stored-key"
	testStoredProvider     = "stored"
	testFallbackProvider   = "fallback"
	testEmptyProvider      = "empty"
	testStoredEnvLike      = "stored-env-like"
	testStoredOAuthAccess  = "stored-oauth-access"
	testStoredOAuthRefresh = "stored-oauth-refresh"
)

func TestStorageResolvesAuthSourcesWithoutExposingSecrets(t *testing.T) {
	t.Parallel()

	storage := testutil.NewAuthStorage(t, map[string]auth.Credential{
		"openai": testAPIKeyCredential(),
	})

	apiKey, found := storage.APIKey("openai")
	require.True(t, found)
	assert.Equal(t, testStoredKey, apiKey)
	assert.Equal(t, auth.Status{Source: auth.SourceStored, Label: "", Configured: true}, storage.AuthStatus("openai"))

	storage.SetRuntimeAPIKey("openai", "runtime-key")
	apiKey, found = storage.APIKey("openai")
	require.True(t, found)
	assert.Equal(t, "runtime-key", apiKey)
	assert.Equal(t,
		auth.Status{Source: auth.SourceRuntime, Label: "--api-key", Configured: false},
		storage.AuthStatus("openai"),
	)

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

func testAPIKeyCredential() auth.Credential {
	return auth.Credential{
		OAuth:     nil,
		Type:      auth.CredentialTypeAPIKey,
		Key:       testStoredKey,
		Access:    "",
		Refresh:   "",
		AccountID: "",
		Expires:   0,
		ExpiresAt: 0,
	}
}

func TestStoragePersistsFileCredentials(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	authPath := filepath.Join(t.TempDir(), "auth.json")
	storage, err := auth.NewStorage(ctx, auth.NewFileBackend(authPath))
	require.NoError(t, err)

	credential := testAPIKeyCredential()
	require.NoError(t, storage.Set(ctx, "openai", &credential))
	assert.Equal(t, []string{"openai"}, storage.List())

	reloaded, err := auth.NewStorage(ctx, auth.NewFileBackend(authPath))
	require.NoError(t, err)

	stored, hasStoredCredential := reloaded.Get("openai")
	require.True(t, hasStoredCredential)
	assert.Equal(t, credential, stored)

	apiKey, found := reloaded.APIKey("openai")
	require.True(t, found)
	assert.Equal(t, testStoredKey, apiKey)

	info, err := os.Stat(authPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	require.NoError(t, reloaded.Remove(ctx, "openai"))
	assert.False(t, reloaded.HasStored("openai"))
	_, hasStoredCredential = reloaded.Get("openai")
	assert.False(t, hasStoredCredential)
}

func TestStorageReportsAuthAvailabilityBySource(t *testing.T) {
	t.Parallel()

	storage := testutil.NewAuthStorage(t, map[string]auth.Credential{
		testStoredEnvLike: {
			OAuth:     nil,
			Type:      auth.CredentialTypeAPIKey,
			Key:       "env-key",
			Access:    "",
			Refresh:   "",
			AccountID: "",
			Expires:   0,
			ExpiresAt: 0,
		},
		testStoredProvider: testAPIKeyCredential(),
		testEmptyProvider: {
			OAuth:     nil,
			Type:      auth.CredentialTypeAPIKey,
			Key:       "",
			Access:    "",
			Refresh:   "",
			AccountID: "",
			Expires:   0,
			ExpiresAt: 0,
		},
	})
	storage.SetRuntimeAPIKey("runtime", "runtime-key")
	storage.SetFallbackResolver(func(provider string) (string, bool) {
		return "fallback-key", provider == testFallbackProvider
	})

	tests := []struct {
		name     string
		provider string
		expected bool
	}{
		{name: "runtime", provider: "runtime", expected: true},
		{name: "stored", provider: testStoredProvider, expected: true},
		{name: testStoredEnvLike, provider: testStoredEnvLike, expected: true},
		{name: "fallback", provider: testFallbackProvider, expected: true},
		{name: "empty", provider: testEmptyProvider, expected: false},
		{name: "missing", provider: "missing", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.expected, storage.HasAuth(tt.provider))
		})
	}
}

func TestStorageDrainsErrors(t *testing.T) {
	t.Parallel()

	backend := &invalidJSONBackend{}
	storage, err := auth.NewStorage(t.Context(), backend)
	require.Error(t, err)
	require.Nil(t, storage)

	credential := testAPIKeyCredential()
	storageBackend := &failingBackend{err: assert.AnError}
	failingStorage, err := auth.NewStorage(t.Context(), storageBackend)
	require.NoError(t, err)
	require.Error(t, failingStorage.Set(t.Context(), "openai", &credential))

	drained := failingStorage.DrainErrors()
	require.Len(t, drained, 1)
	require.ErrorIs(t, drained[0], assert.AnError)
	assert.Empty(t, failingStorage.DrainErrors())
}

func TestStorageFallbackResolverDoesNotRunUnderLock(t *testing.T) {
	t.Parallel()

	storage := testutil.NewAuthStorage(t, map[string]auth.Credential{})

	storage.SetFallbackResolver(func(provider string) (string, bool) {
		storage.SetRuntimeAPIKey(provider, "runtime-from-resolver")

		return "fallback-" + provider, true
	})

	apiKey, found := storage.APIKey("custom")
	require.True(t, found)
	assert.Equal(t, "fallback-custom", apiKey)

	apiKey, found = storage.APIKey("custom")
	require.True(t, found)
	assert.Equal(t, "runtime-from-resolver", apiKey)
}

func TestStorageSeparatesAnthropicAPIAndOAuthEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-api03-env")
	t.Setenv("ANTHROPIC_OAUTH_TOKEN", "sk-ant-oat-env")

	storage := testutil.NewAuthStorage(t, map[string]auth.Credential{})

	apiKey, found := storage.APIKey("anthropic")
	require.True(t, found)
	assert.Equal(t, "sk-ant-api03-env", apiKey)
	assert.Equal(t,
		auth.Status{Source: auth.SourceEnvironment, Label: "ANTHROPIC_API_KEY", Configured: false},
		storage.AuthStatus("anthropic"),
	)

	oauthToken, found := storage.APIKey("anthropic-claude")
	require.True(t, found)
	assert.Equal(t, "sk-ant-oat-env", oauthToken)
	assert.Equal(t,
		auth.Status{Source: auth.SourceEnvironment, Label: "ANTHROPIC_OAUTH_TOKEN", Configured: false},
		storage.AuthStatus("anthropic-claude"),
	)
}

func TestStorageReloadKeepsPreviousCredentialsWhenParseFails(t *testing.T) {
	t.Parallel()

	backend := &mutableBackend{content: []byte(`{"openai":{"type":"api_key","key":"stored-key"}}`)}
	storage, err := auth.NewStorage(t.Context(), backend)
	require.NoError(t, err)
	assert.True(t, storage.HasStored("openai"))

	backend.content = []byte(`{`)
	err = storage.Reload(t.Context())
	require.Error(t, err)
	assert.True(t, storage.HasStored("openai"))
	require.Len(t, storage.DrainErrors(), 1)
}

func TestStorageSetRejectsNilCredential(t *testing.T) {
	t.Parallel()

	storage := testutil.NewAuthStorage(t, map[string]auth.Credential{})

	err := storage.Set(t.Context(), "openai", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "credential is required")
}

func TestStorageAPIKeyContextUsesRuntimeEnvFallbackAndUnknownOAuth(t *testing.T) {
	storage := testutil.NewAuthStorage(t, map[string]auth.Credential{
		"unknown-oauth": {
			OAuth:     nil,
			Type:      auth.CredentialTypeOAuth,
			Key:       "",
			Access:    "unknown-access",
			Refresh:   "",
			AccountID: "",
			Expires:   time.Now().Add(time.Hour).UnixMilli(),
			ExpiresAt: 0,
		},
	})

	storage.SetRuntimeAPIKey("runtime", "runtime-key")
	apiKey, found, err := storage.APIKeyContext(t.Context(), "runtime")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "runtime-key", apiKey)

	t.Setenv("OPENAI_API_KEY", "env-key")
	apiKey, found, err = storage.APIKeyContext(t.Context(), "openai")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "env-key", apiKey)

	storage.SetFallbackResolver(func(provider string) (string, bool) {
		return "fallback-key", provider == "fallback-provider"
	})
	apiKey, found, err = storage.APIKeyContext(t.Context(), "fallback-provider")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "fallback-key", apiKey)

	apiKey, found, err = storage.APIKeyContext(t.Context(), "unknown-oauth")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "unknown-access", apiKey)
}

func TestStorageAPIKeyContextReturnsUnknownProviderOAuthAccess(t *testing.T) {
	t.Parallel()

	credential := auth.Credential{
		OAuth:     nil,
		Type:      auth.CredentialTypeOAuth,
		Key:       "",
		Access:    "custom-access",
		Refresh:   "",
		AccountID: "",
		Expires:   time.Now().Add(time.Hour).UnixMilli(),
		ExpiresAt: 0,
	}
	storage := testutil.NewAuthStorage(t, map[string]auth.Credential{"custom-oauth": credential})

	apiKey, found, err := storage.APIKeyContext(t.Context(), "custom-oauth")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "custom-access", apiKey)
}

func TestStorageUsesStoredOAuthCredentialMaterial(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		apiKey     string
		credential auth.Credential
		found      bool
	}{
		{
			name: "unexpired access token",
			credential: auth.Credential{
				OAuth:     nil,
				Type:      auth.CredentialTypeOAuth,
				Key:       "",
				Access:    testStoredOAuthAccess,
				Refresh:   testStoredOAuthRefresh,
				AccountID: "",
				Expires:   time.Now().Add(time.Hour).UnixMilli(),
				ExpiresAt: 0,
			},
			apiKey: testStoredOAuthAccess,
			found:  true,
		},
		{
			name: "refresh token only reports stored status",
			credential: auth.Credential{
				OAuth:     nil,
				Type:      auth.CredentialTypeOAuth,
				Key:       "",
				Access:    "",
				Refresh:   testStoredOAuthRefresh,
				AccountID: "",
				Expires:   0,
				ExpiresAt: 0,
			},
			apiKey: "",
			found:  false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			storage := testutil.NewAuthStorage(t, map[string]auth.Credential{
				"anthropic-claude": test.credential,
			})

			apiKey, found := storage.APIKey("anthropic-claude")
			assert.Equal(t, test.found, found)
			assert.Equal(t, test.apiKey, apiKey)
			assert.Equal(t,
				auth.Status{Source: auth.SourceStored, Label: "", Configured: true},
				storage.AuthStatus("anthropic-claude"),
			)
		})
	}
}

func TestStorageSetRemoveDoNotMutateMemoryWhenPersistFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	backend := &failingBackend{err: assert.AnError}
	storage, err := auth.NewStorage(ctx, backend)
	require.NoError(t, err)

	credential := testAPIKeyCredential()
	err = storage.Set(ctx, "openai", &credential)
	require.Error(t, err)
	assert.False(t, storage.HasStored("openai"))

	storage.SetRuntimeAPIKey("openai", "runtime-key")
	err = storage.Remove(ctx, "openai")
	require.Error(t, err)
	assert.False(t, storage.HasStored("openai"))
}

type mutableBackend struct {
	content []byte
}

func (backend *mutableBackend) WithLock(
	_ context.Context,
	callback func(current []byte) (auth.LockResult, error),
) error {
	result, err := callback(backend.content)
	if err != nil {
		return err
	}

	if result.Write {
		backend.content = append([]byte{}, result.Next...)
	}

	return nil
}

type failingBackend struct {
	err error
}

func (backend *failingBackend) WithLock(
	_ context.Context,
	callback func(current []byte) (auth.LockResult, error),
) error {
	result, err := callback([]byte("{}"))
	if err != nil {
		return err
	}

	if result.Write {
		return backend.err
	}

	return nil
}

type invalidJSONBackend struct{}

func (backend *invalidJSONBackend) WithLock(
	_ context.Context,
	callback func(current []byte) (auth.LockResult, error),
) error {
	_, err := callback([]byte("{"))

	return err
}
