package di_test

import (
	"context"
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

	container, err := di.NewContainer(context.Background(), configPath, di.ConfigOverrides{
		DisableExtensions: true,
		Interactive:       false,
	})
	require.NoError(t, err)
	t.Cleanup(func() { assert.True(t, container.ShutdownWithContext(t.Context()).Succeed) })

	configService, err := container.ConfigService()
	require.NoError(t, err)

	cfg := configService.Get()
	assert.False(t, cfg.Extensions.Enabled)
}

func TestConfigServiceTracksLoadedPathAndInteractiveOverride(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		interactive bool
	}{
		{name: "non-interactive", interactive: false},
		{name: "interactive", interactive: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			configPath := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(configPath, []byte("app:\n  env: test\n"), 0o600))

			container, err := di.NewContainer(context.Background(), configPath, di.ConfigOverrides{
				DisableExtensions: false,
				Interactive:       test.interactive,
			})
			require.NoError(t, err)
			t.Cleanup(func() { assert.True(t, container.ShutdownWithContext(t.Context()).Succeed) })

			configService, err := container.ConfigService()
			require.NoError(t, err)
			assert.Equal(t, configPath, configService.Path())
			assert.Equal(t, test.interactive, configService.Interactive())
		})
	}
}
