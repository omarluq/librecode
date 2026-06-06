package di

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/samber/do/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/omarluq/librecode/internal/auth"
	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/database"
)

func TestNewModelServiceWiresRegistryDiscovery(t *testing.T) {
	home := t.TempDir()
	t.Setenv("LIBRECODE_HOME", home)
	db := newModelServiceTestDB(t)
	documents := database.NewDocumentRepository(db)
	storage, err := auth.NewInMemoryStorage(t.Context(), map[string]auth.Credential{})
	require.NoError(t, err)
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
		DB:        nil,
		Sessions:  nil,
		Documents: documents,
		path:      "",
	})
	do.ProvideValue(injector, &AuthService{Storage: storage})

	service, err := NewModelService(injector)
	require.NoError(t, err)
	require.NotNil(t, service.Registry)
	assert.NotEmpty(t, service.Registry.All())
	assert.Equal(t, filepath.Join(home, "models-dev.json"), modelDiscoveryCachePath())
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
