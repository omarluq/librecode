package di_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/di"
	"github.com/omarluq/librecode/internal/extension"
)

func TestExtensionServiceUsesProjectLockfile(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LIBRECODE_HOME", filepath.Join(home, ".librecode"))
	t.Chdir(cwd)

	projectConfig := filepath.Join(cwd, ".librecode", "config.yaml")
	projectLock := filepath.Join(cwd, ".librecode", extension.LockFileName)
	writeDIFile(t, projectConfig, []byte("extensions:\n  use:\n    - github:owner/repo\n"))
	lockFile := extension.LockFile{Extensions: map[string]extension.LockedExtension{
		"github:owner/repo": {Resolved: "", Version: "v9.9.9"},
	}}
	require.NoError(t, extension.WriteLockFile(projectLock, lockFile))

	container, err := di.NewContainer("", di.ConfigOverrides{DisableExtensions: false})
	require.NoError(t, err)
	t.Cleanup(func() {
		report := container.ShutdownWithContext(context.Background())
		assert.True(t, report.Succeed, report.Error())
	})

	state := di.MustInvoke[*di.ExtensionService](container).State
	require.Len(t, state.Configured, 1)
	assert.Equal(t, "v9.9.9", state.Configured[0].Lock.Version)
}

func TestExtensionLockPathUsesGlobalPathWhenConfigPathIsEmpty(t *testing.T) {
	t.Parallel()

	home := t.TempDir()

	path := di.ExtensionLockPathForTest("", home)

	assert.Equal(t, filepath.Join(home, extension.LockFileName), path)
}

func TestExtensionLoadPathsDeduplicatesCanonicalPaths(t *testing.T) {
	t.Parallel()

	extensionDir := t.TempDir()
	symlinkPath := filepath.Join(t.TempDir(), "extension")
	createSymlinkOrSkip(t, extensionDir, symlinkPath)

	paths := di.ExtensionLoadPathsForTest([]extension.ResolvedSource{
		newResolvedSourceForTest(extensionDir),
		newResolvedSourceForTest(symlinkPath),
		newResolvedSourceForTest(extensionDir),
	})

	require.Len(t, paths, 1)
	assert.Equal(t, extensionDir, paths[0])
}

func newResolvedSourceForTest(loadPath string) extension.ResolvedSource {
	return extension.ResolvedSource{
		Configured: extension.ConfiguredSource{Source: "path:" + loadPath, Version: ""},
		Ref:        extension.SourceRef{Scheme: "path", Value: loadPath, Version: ""},
		Lock:       extension.LockedExtension{Resolved: "", Version: ""},
		Name:       loadPath,
		LoadPath:   loadPath,
		Status:     "path",
	}
}

func createSymlinkOrSkip(t *testing.T, oldname, newname string) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("skipping symlink test on Windows: symlinks require elevated privileges")
	}
	require.NoError(t, os.Symlink(oldname, newname))
}

func writeDIFile(t *testing.T, path string, content []byte) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, content, 0o600))
}
