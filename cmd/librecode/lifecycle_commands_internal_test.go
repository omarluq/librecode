package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDatabaseCommandsUseApplicationLifecycle(t *testing.T) {
	t.Parallel()

	configPath := writeLifecycleTestConfig(t)

	newOutput := executeLifecycleCommand(t, configPath, "session", "new", "coverage session")
	sessionID := strings.TrimSpace(newOutput)
	require.NotEmpty(t, sessionID)

	listOutput := executeLifecycleCommand(t, configPath, "session", listUse)
	assert.Contains(t, listOutput, sessionID)
	assert.Contains(t, listOutput, "coverage session")

	showOutput := executeLifecycleCommand(t, configPath, "session", "show", sessionID)
	assert.Empty(t, showOutput)

	migrateOutput := executeLifecycleCommand(t, configPath, "migrate")
	assert.Contains(t, migrateOutput, "migrations applied:")
}

func TestExtensionAndToolCommandsUseApplicationLifecycle(t *testing.T) {
	t.Parallel()

	configPath := writeLifecycleTestConfig(t)

	assert.Empty(t, executeLifecycleCommand(t, configPath, "extension", listUse))

	cmd := newRootCmd()
	cmd.SetArgs([]string{configFlag, configPath, noExtensionsFlag, "extension", "run", "missing"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "run extension command")

	toolOutput := executeLifecycleCommand(t, configPath, toolUse, listUse)
	assert.Contains(t, toolOutput, "read\tread_only=true")
}

func writeLifecycleTestConfig(t *testing.T) string {
	t.Helper()

	databasePath := filepath.Join(t.TempDir(), "librecode.db")

	return writeTestConfig(t, fmt.Sprintf(
		"database:\n  path: %q\nmodels:\n  discovery:\n    enabled: false\nextensions:\n  use: []\n",
		databasePath,
	))
}

func executeLifecycleCommand(t *testing.T, configPath string, args ...string) string {
	t.Helper()

	cmd := newRootCmd()
	output := new(bytes.Buffer)
	cmd.SetOut(output)
	cmd.SetArgs(append([]string{configFlag, configPath, noExtensionsFlag}, args...))
	require.NoError(t, cmd.Execute())

	return output.String()
}
