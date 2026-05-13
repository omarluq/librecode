package di_test

import (
	"os"
	"path/filepath"
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
	t.Cleanup(func() { _ = container.ShutdownWithContext(t.Context()).Succeed })

	state := di.MustInvoke[*di.ExtensionService](container).State
	require.Len(t, state.Configured, 1)
	assert.Equal(t, "v9.9.9", state.Configured[0].Lock.Version)
}

func writeDIFile(t *testing.T, path string, content []byte) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, content, 0o600))
}
