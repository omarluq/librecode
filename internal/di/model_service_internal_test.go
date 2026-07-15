package di

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/samber/do/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite" // register SQLite driver for sql.Open in this test

	"github.com/omarluq/librecode/internal/auth"
	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/testutil"
)

func TestNewModelServiceWiresRegistryDiscovery(t *testing.T) {
	home := t.TempDir()
	t.Setenv("LIBRECODE_HOME", home)
	db := newModelServiceTestDB(t)
	documents := database.NewDocumentRepository(db)
	storage := testutil.NewAuthStorage(t, map[string]auth.Credential{})

	cfg := config.Load("").MustGet()
	cfg.Models.Discovery = config.ModelDiscoveryConfig{
		SourceURL:    "https://models.invalid/api.json",
		CacheTTL:     0,
		FetchTimeout: 0,
		Enabled:      false,
	}
	injector := do.New()
	do.ProvideValue(injector, &ConfigService{cfg: cfg, path: ""})
	do.ProvideValue(injector, &DatabaseService{
		DB:         nil,
		Sessions:   nil,
		Documents:  documents,
		Tasks:      nil,
		AgentTasks: nil, Workflows: nil,
		path: "",
	})
	do.ProvideValue(injector, &AuthService{Storage: storage})

	service, err := NewModelService(injector)
	require.NoError(t, err)
	require.NotNil(t, service.Registry)
	assert.NotEmpty(t, service.Registry.All())
	assert.Equal(t, filepath.Join(home, "models-dev.json"), modelDiscoveryCachePath())

	discovery := service.Registry.DiscoveryOptions()
	assert.Equal(t, cfg.Models.Discovery.SourceURL, discovery.SourceURL)
	assert.Equal(t, cfg.Models.Discovery.CacheTTL, discovery.CacheTTL)
	assert.Equal(t, cfg.Models.Discovery.FetchTimeout, discovery.FetchTimeout)
	assert.Equal(t, cfg.Models.Discovery.Enabled, discovery.Enabled)
	assert.Equal(t, filepath.Join(home, "models-dev.json"), discovery.CachePath)
}

func newModelServiceTestDB(t *testing.T) *sql.DB {
	t.Helper()

	connection, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	connection.SetMaxOpenConns(1)
	require.NoError(t, database.Migrate(context.Background(), connection))

	return connection
}
