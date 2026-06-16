//go:build !windows

package tool

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnixShellConfigUsesConfiguredShell(t *testing.T) {
	t.Setenv("SHELL", "/custom/bash")

	shellPath, args, err := shellConfig("echo ok")
	require.NoError(t, err)
	assert.Equal(t, "/custom/bash", shellPath)
	assert.Equal(t, []string{shellLoginArg, "echo ok"}, args)
}

func TestTerminateShellCommandIgnoresMissingProcess(t *testing.T) {
	t.Parallel()

	require.NoError(t, terminateShellCommand(&exec.Cmd{}))
}

func TestKillProcessGroupIgnoresInvalidPID(t *testing.T) {
	t.Parallel()

	require.NoError(t, killProcessGroup(0))
	require.NoError(t, killProcessGroup(-1))
}
