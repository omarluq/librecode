package di_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/di"
)

func TestNewContainer_DisableExtensionsOverride(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("extensions:\n  enabled: true\n"), 0o600))

	container, err := di.NewContainer(configPath, di.ConfigOverrides{DisableExtensions: true})
	require.NoError(t, err)
	t.Cleanup(func() { assert.True(t, container.ShutdownWithContext(t.Context()).Succeed) })

	cfg := di.MustInvoke[*di.ConfigService](container).Get()
	assert.False(t, cfg.Extensions.Enabled)
}

func TestConfigServiceTracksLoadedPath(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("app:\n  env: test\n"), 0o600))

	container, err := di.NewContainer(configPath, di.ConfigOverrides{DisableExtensions: false})
	require.NoError(t, err)
	t.Cleanup(func() { assert.True(t, container.ShutdownWithContext(t.Context()).Succeed) })

	configService := di.MustInvoke[*di.ConfigService](container)
	assert.Equal(t, configPath, configService.Path())
}
