package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/config"
)

func TestLoadPrefersProjectLibrecodeConfig(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LIBRECODE_HOME", filepath.Join(home, ".librecode"))
	t.Chdir(cwd)

	writeConfig(t, filepath.Join(home, ".librecode", "config.yaml"), "app:\n  env: production\n")
	writeConfig(t, filepath.Join(cwd, ".librecode", "config.yaml"), "app:\n  env: test\n")

	cfg := config.Load("").MustGet()
	assert.Equal(t, "test", cfg.App.Env)
}

func TestLoadFallsBackToLibrecodeHomeConfig(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LIBRECODE_HOME", filepath.Join(home, ".librecode"))
	t.Chdir(cwd)

	writeConfig(t, filepath.Join(home, ".librecode", "config.yaml"), "app:\n  env: production\n")

	cfg := config.Load("").MustGet()
	assert.Equal(t, "production", cfg.App.Env)
}

func TestLoadIgnoresRootAndXDGConfig(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	xdgConfig := filepath.Join(home, ".xdg-config")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdgConfig)
	t.Setenv("LIBRECODE_HOME", filepath.Join(home, ".librecode"))
	t.Chdir(cwd)

	writeConfig(t, filepath.Join(cwd, "config.yaml"), "app:\n  env: test\n")
	writeConfig(t, filepath.Join(xdgConfig, "librecode", "config.yaml"), "app:\n  env: production\n")

	cfg := config.Load("").MustGet()
	assert.Equal(t, "development", cfg.App.Env)
}

func TestLoadParsesExtensionUseForms(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LIBRECODE_HOME", filepath.Join(home, ".librecode"))
	t.Chdir(cwd)

	writeConfig(t, filepath.Join(cwd, ".librecode", "config.yaml"), `database:
  conn_max_lifetime: 45s
cache:
  ttl: 2m
ksql:
  timeout: 3s
assistant:
  retry:
    base_delay: 4s
    max_delay: 8s
extensions:
  use:
    - official:vim-mode
    - github:example/librecode-extension
    - source: github:example/another-extension
      version: v1.2.3
`)

	cfg := config.Load("").MustGet()
	require.Len(t, cfg.Extensions.Use, 3)
	assert.Equal(t, "official:vim-mode", cfg.Extensions.Use[0].Source)
	assert.Equal(t, "github:example/librecode-extension", cfg.Extensions.Use[1].Source)
	assert.Equal(t, "github:example/another-extension", cfg.Extensions.Use[2].Source)
	assert.Equal(t, "v1.2.3", cfg.Extensions.Use[2].Version)
	assert.Equal(t, "45s", cfg.Database.ConnMaxLifetime.String())
	assert.Equal(t, "2m0s", cfg.Cache.TTL.String())
	assert.Equal(t, "3s", cfg.KSQL.Timeout.String())
	assert.Equal(t, "4s", cfg.Assistant.Retry.BaseDelay.String())
	assert.Equal(t, "8s", cfg.Assistant.Retry.MaxDelay.String())
	assert.True(t, cfg.Context.PreflightEnabled)
	assert.Equal(t, 2048, cfg.Context.ProviderReserveTokens)
	assert.Equal(t, 8192, cfg.Context.SafetyMarginTokens)
	assert.True(t, cfg.Models.Discovery.Enabled)
	assert.Equal(t, "https://models.dev/api.json", cfg.Models.Discovery.SourceURL)
	assert.Equal(t, "24h0m0s", cfg.Models.Discovery.CacheTTL.String())
	assert.Equal(t, "10s", cfg.Models.Discovery.FetchTimeout.String())
}

func TestLoadRejectsInvalidModelsDiscoveryConfig(t *testing.T) {
	testCases := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name:     "empty source url",
			content:  "models:\n  discovery:\n    source_url: ''\n",
			expected: "models.discovery.source_url is required",
		},
		{
			name:     "negative cache ttl",
			content:  "models:\n  discovery:\n    cache_ttl: -1s\n",
			expected: "models.discovery.cache_ttl cannot be negative",
		},
		{
			name:     "negative fetch timeout",
			content:  "models:\n  discovery:\n    fetch_timeout: -1s\n",
			expected: "models.discovery.fetch_timeout cannot be negative",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			home := t.TempDir()
			cwd := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("LIBRECODE_HOME", filepath.Join(home, ".librecode"))
			t.Chdir(cwd)

			writeConfig(t, filepath.Join(cwd, ".librecode", "config.yaml"), testCase.content)

			result := config.Load("")
			assert.True(t, result.IsError())
			assert.ErrorContains(t, result.Error(), testCase.expected)
		})
	}
}

func TestLoadRejectsEmptyExtensionUseObject(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LIBRECODE_HOME", filepath.Join(home, ".librecode"))
	t.Chdir(cwd)

	writeConfig(t, filepath.Join(cwd, ".librecode", "config.yaml"), `extensions:
  use:
    - version: v1.2.3
`)

	result := config.Load("")
	assert.True(t, result.IsError())
	assert.ErrorContains(t, result.Error(), "extensions.use source is required")
}

func TestLoadRejectsInvalidExtensionUseSource(t *testing.T) {
	testCases := []struct {
		name   string
		source string
	}{
		{name: "invalid github shorthand", source: "github:owner"},
		{name: "unsupported scheme", source: "npm:package"},
		{name: "github traversal", source: "github:owner/repo//../bad"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			home := t.TempDir()
			cwd := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("LIBRECODE_HOME", filepath.Join(home, ".librecode"))
			t.Chdir(cwd)

			content := "extensions:\n  use:\n    - " + testCase.source + "\n"
			writeConfig(t, filepath.Join(cwd, ".librecode", "config.yaml"), content)

			result := config.Load("")
			assert.True(t, result.IsError())
			assert.ErrorContains(t, result.Error(), `config: invalid extensions.use source`)
		})
	}
}

func writeConfig(t *testing.T, path, content string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}
