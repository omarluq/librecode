package extension_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/extension"
)

func TestReadLockFile(t *testing.T) {
	t.Parallel()

	runReadLockFileTest(t, "missing file returns empty", func(t *testing.T, dir string) string {
		t.Helper()

		return lockFilePath(t, dir)
	}, extension.LockFile{Extensions: map[string]extension.LockedExtension{}}, "")

	runReadLockFileTest(t, "rejects invalid YAML", func(t *testing.T, dir string) string {
		t.Helper()
		path := lockFilePath(t, dir)
		require.NoError(t, os.WriteFile(path, []byte("extensions: ["), 0o600))

		return path
	}, emptyLockFile(), "parse lockfile")

	runReadLockFileTest(t, "normalizes nil extensions", func(t *testing.T, dir string) string {
		t.Helper()
		path := lockFilePath(t, dir)
		require.NoError(t, os.WriteFile(path, []byte("{}\n"), 0o600))

		return path
	}, extension.LockFile{Extensions: map[string]extension.LockedExtension{}}, "")

	runReadLockFileTest(t, "reads pinned extensions", func(t *testing.T, dir string) string {
		t.Helper()
		path := lockFilePath(t, dir)
		require.NoError(t, os.WriteFile(path, []byte(`extensions:
  github:owner/repo:
    resolved: github:owner/repo//extensions/vim-mode
    version: v1.2.3
`), 0o600))

		return path
	}, pinnedLockFile(), "")

	runReadLockFileTest(t, "reports read errors", func(_ *testing.T, dir string) string {
		return dir
	}, emptyLockFile(), "read lockfile")
}

func runReadLockFileTest(
	t *testing.T,
	name string,
	setup func(t *testing.T, dir string) string,
	want extension.LockFile,
	wantErr string,
) {
	t.Helper()

	t.Run(name, func(t *testing.T) {
		t.Parallel()

		got, err := extension.ReadLockFile(setup(t, t.TempDir()))
		if wantErr != "" {
			assert.ErrorContains(t, err, wantErr)

			return
		}

		require.NoError(t, err)
		assert.Equal(t, want, got)
	})
}

func emptyLockFile() extension.LockFile {
	return extension.LockFile{Extensions: map[string]extension.LockedExtension{}}
}

func pinnedLockFile() extension.LockFile {
	return extension.LockFile{Extensions: map[string]extension.LockedExtension{
		"github:owner/repo": {
			Resolved: "github:owner/repo//extensions/vim-mode",
			Version:  "v1.2.3",
		},
	}}
}

func lockFilePath(t *testing.T, dir string) string {
	t.Helper()

	return filepath.Join(dir, extension.LockFileName)
}
