package execpath

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	executableTestMode = 0o700
	timeoutTestDelay   = 10 * time.Millisecond
)

func TestFindRejectsWhitespaceOnlyName(t *testing.T) {
	t.Parallel()

	path, err := Find(" ")
	require.Error(t, err)
	assert.Empty(t, path)
}

func TestFindRejectsRelativePath(t *testing.T) {
	t.Parallel()

	path, err := Find(filepath.Join("local", "tool"))
	require.Error(t, err)
	assert.Empty(t, path)
}

func TestFindDoesNotUsePATH(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("test relies on Unix executable mode")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "private-tool")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"), executableTestMode))
	t.Setenv("PATH", dir)

	found, err := Find("private-tool")
	require.Error(t, err)
	assert.Empty(t, found)
}

func TestCommandReportsMissingExecutable(t *testing.T) {
	t.Parallel()

	cmd, err := Command("definitely-not-a-real-tool")
	require.Error(t, err)
	assert.Nil(t, cmd)
}

func TestRunWithTimeoutReportsDeadlineExceeded(t *testing.T) {
	t.Parallel()

	cmd := exec.CommandContext(t.Context(), "sh", "-c", "sleep 1")
	err := RunWithTimeout(cmd, timeoutTestDelay)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestRunWithTimeoutWrapsRunFailure(t *testing.T) {
	t.Parallel()

	err := RunWithTimeout(exec.CommandContext(t.Context(), "definitely-not-a-command"), 0)
	require.Error(t, err)
	assert.NotErrorIs(t, err, context.DeadlineExceeded)
}

func TestFindRejectsAbsoluteExecutableOutsideFixedDirs(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == windowsOS {
		t.Skip("test relies on Unix executable mode")
	}

	path := filepath.Join(t.TempDir(), "tool")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"), executableTestMode))

	found, err := Find(path)
	require.Error(t, err)
	assert.Empty(t, found)
}

func TestIsPathInDir(t *testing.T) {
	t.Parallel()

	assert.True(t, isPathInDir(filepath.Join("root", "bin", "tool"), filepath.Join("root", "bin")))
	assert.False(t, isPathInDir(filepath.Join("root", "binary", "tool"), filepath.Join("root", "bin")))
}
