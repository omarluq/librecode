package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigShowCommandPrintsResolvedConfig(t *testing.T) {
	t.Parallel()

	configPath := writeTestConfig(t, "database:\n  busy_timeout: 7s\nextensions:\n  use: []\n")
	cmd := rootWrappedCommand(newConfigShowCmd(), configPath)
	output := new(bytes.Buffer)
	cmd.SetOut(output)

	require.NoError(t, cmd.Execute())
	assert.Contains(t, output.String(), "Environment: development")
	assert.Contains(t, output.String(), "database.busy_timeout")
	assert.Contains(t, output.String(), "7s")
}

func TestConfigValidateCommandPrintsSuccess(t *testing.T) {
	t.Parallel()

	configPath := writeTestConfig(t, "extensions:\n  use: []\n")
	cmd := rootWrappedCommand(newConfigValidateCmd(), configPath)
	output := new(bytes.Buffer)
	cmd.SetOut(output)

	require.NoError(t, cmd.Execute())
	assert.Equal(t, "configuration is valid\n", output.String())
}

func TestLoadConfigWrapsInvalidConfig(t *testing.T) {
	t.Parallel()

	configPath := writeTestConfig(t, "database:\n  busy_timeout: -1s\nextensions:\n  use: []\n")
	_, err := loadConfig(configPath)
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

func rootWrappedCommand(child *cobra.Command, configPath string) *cobra.Command {
	root := &cobra.Command{Use: "root"}
	root.PersistentFlags().String("config", configPath, "config file path")
	root.PersistentFlags().Bool("no-extensions", false, "disable Lua extensions for this run")
	root.AddCommand(child)
	root.SetArgs([]string{strings.Fields(child.Use)[0]})

	return root
}
