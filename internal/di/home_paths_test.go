package di_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/di"
)

func TestAuthServicePrefersProjectLibrecodeAuth(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LIBRECODE_HOME", filepath.Join(home, ".librecode"))
	t.Chdir(cwd)

	projectAuthPath := filepath.Join(cwd, ".librecode", "auth.json")
	globalAuthPath := filepath.Join(home, ".librecode", "auth.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(projectAuthPath), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Dir(globalAuthPath), 0o700))
	projectAuth := []byte(`{"project-provider":{"type":"api_key","key":"project-key"}}`)
	globalAuth := []byte(`{"global-provider":{"type":"api_key","key":"global-key"}}`)
	require.NoError(t, os.WriteFile(projectAuthPath, projectAuth, 0o600))
	require.NoError(t, os.WriteFile(globalAuthPath, globalAuth, 0o600))

	container, err := di.NewContainer("", di.ConfigOverrides{DisableExtensions: false})
	require.NoError(t, err)
	storage := di.MustInvoke[*di.AuthService](container).Storage
	t.Cleanup(func() { require.True(t, container.ShutdownWithContext(t.Context()).Succeed) })

	assert.True(t, storage.HasStored("project-provider"))
	assert.False(t, storage.HasStored("global-provider"))
}

func TestDatabaseServicePrefersProjectLibrecodeDatabase(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LIBRECODE_HOME", filepath.Join(home, ".librecode"))
	t.Chdir(cwd)

	projectPath := filepath.Join(cwd, ".librecode", "librecode.db")
	require.NoError(t, os.MkdirAll(filepath.Dir(projectPath), 0o700))
	require.NoError(t, os.WriteFile(projectPath, nil, 0o600))

	container, err := di.NewContainer("", di.ConfigOverrides{DisableExtensions: false})
	require.NoError(t, err)
	databaseService := di.MustInvoke[*di.DatabaseService](container)
	t.Cleanup(func() { _ = container.ShutdownWithContext(t.Context()).Succeed })

	assert.Equal(t, projectPath, databaseService.Path())
}
