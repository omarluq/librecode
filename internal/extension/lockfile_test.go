package extension_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/extension"
)

func TestReadLockFileMissingReturnsEmptyLock(t *testing.T) {
	t.Parallel()

	lockFile, err := extension.ReadLockFile(filepath.Join(t.TempDir(), extension.LockFileName))

	require.NoError(t, err)
	assert.Empty(t, lockFile.Extensions)
}

func TestReadLockFileRejectsInvalidYAML(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), extension.LockFileName)
	require.NoError(t, os.WriteFile(path, []byte("extensions: ["), 0o600))

	_, err := extension.ReadLockFile(path)

	assert.ErrorContains(t, err, "parse lockfile")
}

func TestReadLockFileNormalizesNilExtensions(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), extension.LockFileName)
	require.NoError(t, os.WriteFile(path, []byte("{}\n"), 0o600))

	lockFile, err := extension.ReadLockFile(path)

	require.NoError(t, err)
	assert.NotNil(t, lockFile.Extensions)
	assert.Empty(t, lockFile.Extensions)
}

func TestReadLockFileReportsReadErrors(t *testing.T) {
	t.Parallel()

	_, err := extension.ReadLockFile(t.TempDir())

	assert.ErrorContains(t, err, "read lockfile")
}

func TestWriteLockFileInitializesNilExtensionsAndSetsPrivateMode(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), extension.LockFileName)

	require.NoError(t, extension.WriteLockFile(path, extension.LockFile{Extensions: nil}))

	loaded, err := extension.ReadLockFile(path)
	require.NoError(t, err)
	assert.Empty(t, loaded.Extensions)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestWriteLockFileFailsWhenParentIsFile(t *testing.T) {
	t.Parallel()

	parentPath := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(parentPath, []byte("file"), 0o600))

	path := filepath.Join(parentPath, extension.LockFileName)

	err := extension.WriteLockFile(path, extension.LockFile{Extensions: nil})

	assert.ErrorContains(t, err, "create lockfile dir")
}
