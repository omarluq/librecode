package main

import (
	"bytes"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:paralleltest // Uses package-level cfgFile flag state.
func TestConfigShowCommandPrintsResolvedConfig(t *testing.T) {
	configPath := writeTestConfig(t, "database:\n  busy_timeout: 7s\nextensions:\n  use: []\n")
	withConfigFile(t, configPath)

	cmd := newConfigShowCmd()
	output := new(bytes.Buffer)
	cmd.SetOut(output)

	require.NoError(t, cmd.Execute())
	assert.Contains(t, output.String(), "Environment: development")
	assert.Contains(t, output.String(), "database.busy_timeout")
	assert.Contains(t, output.String(), "7s")
}

//nolint:paralleltest // Uses package-level cfgFile flag state.
func TestConfigValidateCommandPrintsSuccess(t *testing.T) {
	configPath := writeTestConfig(t, "extensions:\n  use: []\n")
	withConfigFile(t, configPath)

	cmd := newConfigValidateCmd()
	output := new(bytes.Buffer)
	cmd.SetOut(output)

	require.NoError(t, cmd.Execute())
	assert.Equal(t, "configuration is valid\n", output.String())
}

//nolint:paralleltest // Uses package-level cfgFile flag state.
func TestLoadConfigWrapsInvalidConfig(t *testing.T) {
	configPath := writeTestConfig(t, "database:\n  busy_timeout: -1s\nextensions:\n  use: []\n")
	withConfigFile(t, configPath)

	_, err := loadConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load configuration")
	assert.Contains(t, err.Error(), "database.busy_timeout cannot be negative")
}

func writeTestConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	writeCLIFile(t, path, content)

	return path
}

var cfgFileTestMu sync.Mutex

func withConfigFile(t *testing.T, path string) {
	t.Helper()

	cfgFileTestMu.Lock()
	previous := cfgFile
	cfgFile = path
	t.Cleanup(func() {
		cfgFile = previous
		cfgFileTestMu.Unlock()
	})
}
