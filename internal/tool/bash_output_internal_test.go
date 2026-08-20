package tool

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteFullBashOutputUsesPrivateCacheFile(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheDir)

	outputPath, err := writeFullBashOutput([]byte("hello\nworld"))
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(cacheDir, "librecode", "bash-output"), filepath.Dir(outputPath))
	info, err := os.Stat(filepath.Dir(outputPath))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())

	content, err := os.ReadFile(filepath.Clean(outputPath))
	require.NoError(t, err)
	assert.Equal(t, "hello\nworld", string(content))
}

func TestBashOutputFSErrorPreservesCause(t *testing.T) {
	t.Parallel()

	cause := errors.New("disk full")
	err := bashOutputFSError(cause, "write full bash output")

	require.Error(t, err)
	require.ErrorIs(t, err, cause)
	assert.Contains(t, err.Error(), "write full bash output")
}

func TestBashOutputFormattingHelpers(t *testing.T) {
	t.Parallel()

	require.NoError(t, bashOutputCleanupError(nil, "cleanup"))

	cause := errors.New("cleanup failed")
	err := bashOutputCleanupError(cause, "cleanup")
	require.Error(t, err)
	require.ErrorIs(t, err, cause)

	assert.Equal(t, 3, lastLineByteCount("one\ntwo"))
	assert.Equal(t, 3, lastLineByteCount("two"))
	assert.Equal(t, "status", appendStatus("", "status"))
	assert.Equal(t, "output\n\nstatus", appendStatus("output", "status"))
}

func TestCleanupStaleBashOutputsRemovesOldLogsOnly(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	now := time.Now()

	oldLog := writeBashOutputLogFile(t, outputDir, "librecode-bash-old.log", now.Add(-8*24*time.Hour))
	edgeLog := writeBashOutputLogFile(t, outputDir, "librecode-bash-edge.log", now.Add(-fullBashOutputRetention))
	freshLog := writeBashOutputLogFile(t, outputDir, "librecode-bash-fresh.log", now.Add(-time.Hour))
	keepLog := writeBashOutputLogFile(t, outputDir, "librecode-bash-keep.log", now.Add(-8*24*time.Hour))
	unrelatedFile := writeBashOutputLogFile(t, outputDir, "keep-me.txt", now.Add(-30*24*time.Hour))
	require.NoError(t, os.Mkdir(filepath.Join(outputDir, "librecode-bash-dir"), secureDirMode))

	cleanupStaleBashOutputs(outputDir, keepLog, now)

	assert.NoFileExists(t, oldLog)
	assert.NoFileExists(t, edgeLog)
	assert.FileExists(t, freshLog)
	assert.FileExists(t, keepLog)
	assert.FileExists(t, unrelatedFile)
	assert.DirExists(t, filepath.Join(outputDir, "librecode-bash-dir"))
}

func TestCleanupStaleBashOutputsToleratesMissingDir(t *testing.T) {
	t.Parallel()

	cleanupStaleBashOutputs(filepath.Join(t.TempDir(), "does-not-exist"), "", time.Now())
}

func TestWriteFullBashOutputCleansStaleLogs(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheDir)

	outputDir := filepath.Join(cacheDir, "librecode", "bash-output")
	require.NoError(t, os.MkdirAll(outputDir, secureDirMode))

	stale := writeBashOutputLogFile(t, outputDir, "librecode-bash-stale.log", time.Now().Add(-8*24*time.Hour))

	outputPath, err := writeFullBashOutput([]byte("fresh output"))
	require.NoError(t, err)

	assert.NoFileExists(t, stale)
	assert.FileExists(t, outputPath)
}

func writeBashOutputLogFile(t *testing.T, dir, name string, modTime time.Time) string {
	t.Helper()

	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(name), privateFileMode))
	require.NoError(t, os.Chtimes(path, modTime, modTime))

	return path
}
