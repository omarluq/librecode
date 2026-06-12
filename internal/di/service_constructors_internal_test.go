package di

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/samber/do/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/config"
)

func TestNewCacheServiceUsesConfiguredCache(t *testing.T) {
	t.Parallel()

	injector := do.New()
	do.ProvideValue(injector, &ConfigService{cfg: testServiceConfig(), path: ""})

	service, err := NewCacheService(injector)
	require.NoError(t, err)
	require.NotNil(t, service.Responses)
	t.Cleanup(service.Shutdown)
}

//nolint:paralleltest // t.Chdir cannot run in parallel tests.
func TestNewToolServiceUsesCurrentWorkingDirectory(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)

	service, err := NewToolService(do.New())
	require.NoError(t, err)
	require.NotNil(t, service.Registry)

	absoluteCWD, err := filepath.Abs(cwd)
	require.NoError(t, err)
	assert.Equal(t, absoluteCWD, service.Registry.CWD())
}

func testServiceConfig() *config.Config {
	cfg := config.Load("").MustGet()
	cfg.Cache.Enabled = true
	cfg.Cache.Capacity = 2
	cfg.Cache.TTL = time.Minute

	return cfg
}
