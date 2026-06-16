package model

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/auth"
	"github.com/omarluq/librecode/internal/testutil"
)

const (
	registryTestHeader      = "X-Test"
	registryTestHeaderValue = "yes"
)

func TestRegistryRequestAuthBranches(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(&RegistryOptions{
		ConfigReader: nil,
		Auth:         nil,
		ModelsPath:   "",
		BuiltIns:     []Model{},
		Discovery:    disabledDiscoveryOptions(),
	})
	registry.providerConfigs = map[string]providerRequestConfig{
		"configured": {
			Headers:    map[string]string{registryTestHeader: registryTestHeaderValue},
			APIKey:     "from-config",
			AuthHeader: true,
		},
		"requires-key": {
			Headers:    map[string]string{registryTestHeader: registryTestHeaderValue},
			APIKey:     "",
			AuthHeader: true,
		},
	}

	configured := registry.RequestAuth("configured")
	require.True(t, configured.OK)
	assert.Equal(t, "from-config", configured.APIKey)
	assert.Equal(t, map[string]string{registryTestHeader: registryTestHeaderValue}, configured.Headers)
	configured.Headers[registryTestHeader] = "changed"
	assert.Equal(t, registryTestHeaderValue, registry.providerConfigs["configured"].Headers[registryTestHeader])

	missing := registry.RequestAuth("requires-key")
	assert.False(t, missing.OK)
	assert.Equal(t, "missing API key", missing.Error)
}

func TestRegistryRequestAuthContext(t *testing.T) {
	t.Parallel()

	storage := testutil.NewAuthStorage(t, map[string]auth.Credential{})
	storage.SetRuntimeAPIKey("stored", "runtime-key")
	registry := NewRegistry(&RegistryOptions{
		ConfigReader: nil,
		Auth:         storage,
		ModelsPath:   "",
		BuiltIns:     []Model{},
		Discovery:    disabledDiscoveryOptions(),
	})
	registry.providerConfigs = map[string]providerRequestConfig{
		"stored": {
			Headers:    map[string]string{registryTestHeader: registryTestHeaderValue},
			APIKey:     "from-config",
			AuthHeader: false,
		},
	}

	resolved := registry.RequestAuthContext(t.Context(), "stored")
	require.True(t, resolved.OK)
	assert.Equal(t, "runtime-key", resolved.APIKey)
	assert.Equal(t, map[string]string{registryTestHeader: registryTestHeaderValue}, resolved.Headers)

	missing := registry.RequestAuthContext(t.Context(), "missing")
	assert.False(t, missing.OK)
	assert.Equal(t, "missing API key for provider missing", missing.Error)
}

func TestRegistryRequestAuthContextReturnsAuthErrors(t *testing.T) {
	t.Parallel()

	storage := testutil.NewAuthStorage(t, map[string]auth.Credential{})
	storage.SetFallbackResolver(func(string) (string, bool) {
		return "", false
	})
	registry := NewRegistry(&RegistryOptions{
		ConfigReader: nil,
		Auth:         storage,
		ModelsPath:   "",
		BuiltIns:     []Model{},
		Discovery:    disabledDiscoveryOptions(),
	})
	registry.providerConfigs = map[string]providerRequestConfig{}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	resolved := registry.RequestAuthContext(ctx, "openai-codex")
	assert.False(t, resolved.OK)
}

func TestRegistryOptionsAndFirstRegistryError(t *testing.T) {
	t.Parallel()

	defaults := registryOptions(nil)
	assert.NotEmpty(t, defaults.BuiltIns)
	assert.False(t, defaults.Discovery.Enabled)

	withDefaultBuiltIns := registryOptions(&RegistryOptions{
		ConfigReader: nil,
		Auth:         nil,
		ModelsPath:   "",
		BuiltIns:     nil,
		Discovery:    disabledDiscoveryOptions(),
	})
	assert.NotEmpty(t, withDefaultBuiltIns.BuiltIns)

	withCustomBuiltIns := registryOptions(&RegistryOptions{
		ConfigReader: nil,
		Auth:         nil,
		ModelsPath:   "",
		BuiltIns: []Model{{
			ThinkingLevelMap: nil,
			Headers:          nil,
			Compat:           nil,
			Provider:         "p",
			ID:               "m",
			Name:             "",
			API:              "",
			BaseURL:          "",
			Input:            nil,
			Cost:             Cost{Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0},
			ContextWindow:    0,
			MaxTokens:        0,
			Reasoning:        false,
		}},
		Discovery: disabledDiscoveryOptions(),
	})
	assert.Len(t, withCustomBuiltIns.BuiltIns, 1)

	left := errors.New("left")
	right := errors.New("right")
	require.ErrorIs(t, firstRegistryError(left, right), left)
	require.ErrorIs(t, firstRegistryError(nil, right), right)
	require.NoError(t, firstRegistryError(nil, nil))
}

func disabledDiscoveryOptions() DiscoveryOptions {
	return DiscoveryOptions{
		Client:       nil,
		CachePath:    "",
		SourceURL:    "",
		CacheTTL:     0,
		FetchTimeout: 0,
		Enabled:      false,
	}
}
