package tool

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

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

	content, err := os.ReadFile(outputPath) //nolint:gosec // path is returned by os.CreateTemp in the test cache dir.
	require.NoError(t, err)
	assert.Equal(t, "hello\nworld", string(content))
}

func TestBashOutputFSErrorPreservesCause(t *testing.T) {
	t.Parallel()

	cause := errors.New("disk full")
	err := bashOutputFSError(cause, "write full bash output")

	require.Error(t, err)
	assert.ErrorIs(t, err, cause)
	assert.Contains(t, err.Error(), "write full bash output")
}
