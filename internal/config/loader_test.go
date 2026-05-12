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

func writeConfig(t *testing.T, path, content string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}
