package extension_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/extension"
)

type lockFileCase struct {
	setup  func(t *testing.T, dir string) string
	run    func(path string) (extension.LockFile, error)
	assert func(t *testing.T, path string, lockFile extension.LockFile, err error)
	name   string
}

func TestLockFileReadWrite(t *testing.T) {
	t.Parallel()

	for _, testCase := range lockFileCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			path := testCase.setup(t, t.TempDir())
			lockFile, err := testCase.run(path)

			testCase.assert(t, path, lockFile, err)
		})
	}
}

func lockFileCases() []lockFileCase {
	return []lockFileCase{
		missingLockFileCase(),
		invalidLockFileCase(),
		nilExtensionsLockFileCase(),
		readErrorLockFileCase(),
		writeLockFileCase(),
		writeParentFileLockFileCase(),
	}
}

func missingLockFileCase() lockFileCase {
	return lockFileCase{
		name:  "missing file returns empty",
		setup: lockFilePath,
		run:   extension.ReadLockFile,
		assert: func(t *testing.T, _ string, lockFile extension.LockFile, err error) {
			t.Helper()
			require.NoError(t, err)
			assert.Empty(t, lockFile.Extensions)
		},
	}
}

func invalidLockFileCase() lockFileCase {
	return lockFileCase{
		name: "rejects invalid YAML",
		setup: func(t *testing.T, dir string) string {
			t.Helper()
			path := lockFilePath(t, dir)
			require.NoError(t, os.WriteFile(path, []byte("extensions: ["), 0o600))
			return path
		},
		run: extension.ReadLockFile,
		assert: func(t *testing.T, _ string, _ extension.LockFile, err error) {
			t.Helper()
			assert.ErrorContains(t, err, "parse lockfile")
		},
	}
}

func nilExtensionsLockFileCase() lockFileCase {
	return lockFileCase{
		name: "normalizes nil extensions",
		setup: func(t *testing.T, dir string) string {
			t.Helper()
			path := lockFilePath(t, dir)
			require.NoError(t, os.WriteFile(path, []byte("{}\n"), 0o600))
			return path
		},
		run: extension.ReadLockFile,
		assert: func(t *testing.T, _ string, lockFile extension.LockFile, err error) {
			t.Helper()
			require.NoError(t, err)
			assert.NotNil(t, lockFile.Extensions)
			assert.Empty(t, lockFile.Extensions)
		},
	}
}

func readErrorLockFileCase() lockFileCase {
	return lockFileCase{
		name: "reports read errors",
		setup: func(t *testing.T, dir string) string {
			t.Helper()
			return dir
		},
		run: extension.ReadLockFile,
		assert: func(t *testing.T, _ string, _ extension.LockFile, err error) {
			t.Helper()
			assert.ErrorContains(t, err, "read lockfile")
		},
	}
}

func writeLockFileCase() lockFileCase {
	return lockFileCase{
		name:  "write initializes nil extensions and sets private mode",
		setup: lockFilePath,
		run: func(path string) (extension.LockFile, error) {
			if err := extension.WriteLockFile(path, extension.LockFile{Extensions: nil}); err != nil {
				return extension.LockFile{}, err
			}
			return extension.ReadLockFile(path)
		},
		assert: func(t *testing.T, path string, lockFile extension.LockFile, err error) {
			t.Helper()
			require.NoError(t, err)
			assert.Empty(t, lockFile.Extensions)
			assertLockFileMode(t, path)
		},
	}
}

func writeParentFileLockFileCase() lockFileCase {
	return lockFileCase{
		name: "write fails when parent is file",
		setup: func(t *testing.T, dir string) string {
			t.Helper()
			parentPath := filepath.Join(dir, "not-a-dir")
			require.NoError(t, os.WriteFile(parentPath, []byte("file"), 0o600))
			return filepath.Join(parentPath, extension.LockFileName)
		},
		run: func(path string) (extension.LockFile, error) {
			return extension.LockFile{}, extension.WriteLockFile(path, extension.LockFile{Extensions: nil})
		},
		assert: func(t *testing.T, _ string, _ extension.LockFile, err error) {
			t.Helper()
			assert.ErrorContains(t, err, "create lockfile dir")
		},
	}
}

func lockFilePath(t *testing.T, dir string) string {
	t.Helper()
	return filepath.Join(dir, extension.LockFileName)
}

func assertLockFileMode(t *testing.T, path string) {
	t.Helper()

	info, err := os.Stat(path)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		return
	}
	assert.False(t, info.IsDir())
}
