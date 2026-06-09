package di

import (
	"path/filepath"

	"github.com/samber/do/v2"

	"github.com/omarluq/librecode/internal/core"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

// ModelService owns the provider/model registry.
type ModelService struct {
	Registry *model.Registry
}

// NewModelService wires librecode-style model registry loading.
func NewModelService(injector do.Injector) (*ModelService, error) {
	configService := do.MustInvoke[*ConfigService](injector)
	databaseService := do.MustInvoke[*DatabaseService](injector)
	authStorage := do.MustInvoke[*AuthService](injector).Storage
	registry := model.NewRegistry(&model.RegistryOptions{
		ConfigReader: database.NewDocumentSource(databaseService.Documents, "model", "models"),
		Auth:         authStorage,
		ModelsPath:   "",
		BuiltIns:     nil,
		Discovery: model.DiscoveryOptions{
			Client:       nil,
			CachePath:    modelDiscoveryCachePath(),
			SourceURL:    configService.Get().Models.Discovery.SourceURL,
			CacheTTL:     configService.Get().Models.Discovery.CacheTTL,
			FetchTimeout: configService.Get().Models.Discovery.FetchTimeout,
			Enabled:      configService.Get().Models.Discovery.Enabled,
		},
	})

	return &ModelService{Registry: registry}, nil
}

// modelDiscoveryCachePath returns the models-dev.json cache file path.
// If core.LibrecodeHome fails, it returns an empty string to disable the on-disk cache gracefully.
func modelDiscoveryCachePath() string {
	home, err := core.LibrecodeHome()
	if err != nil {
		return ""
	}

	return filepath.Join(home, "models-dev.json")
}
