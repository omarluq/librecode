package di

import (
	"github.com/samber/do/v2"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

// ModelService owns the provider/model registry.
type ModelService struct {
	Registry *model.Registry
}

// NewModelService wires Pi-style model registry loading.
func NewModelService(injector do.Injector) (*ModelService, error) {
	databaseService := do.MustInvoke[*DatabaseService](injector)
	documentRepository := database.NewDocumentRepository(databaseService.DB)
	authStorage := do.MustInvoke[*AuthService](injector).Storage
	registry := model.NewRegistry(&model.RegistryOptions{
		ConfigSource: database.NewDocumentSource(documentRepository, "model", "models"),
		Auth:         authStorage,
		ModelsPath:   "",
		BuiltIns:     nil,
	})

	return &ModelService{Registry: registry}, nil
}
