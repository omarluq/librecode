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
	home := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LIBRECODE_HOME", filepath.Join(home, ".librecode"))
	t.Chdir(cwd)

	writeConfig(t, filepath.Join(cwd, ".librecode", "config.yaml"), `extensions:
  use:
    - github:owner
`)

	result := config.Load("")
	assert.True(t, result.IsError())
	assert.ErrorContains(t, result.Error(), `config: invalid extensions.use source "github:owner"`)
}

func writeConfig(t *testing.T, path, content string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}
