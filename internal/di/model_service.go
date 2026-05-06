package di

import (
	"github.com/samber/do/v2"

	"github.com/omarluq/librecode/internal/model"
)

// ModelService owns the provider/model registry.
type ModelService struct {
	Registry *model.Registry
}

// NewModelService wires Pi-style model registry loading.
func NewModelService(injector do.Injector) (*ModelService, error) {
	modelsPath, err := userConfigPath("models.json")
	if err != nil {
		return nil, err
	}
	authStorage := do.MustInvoke[*AuthService](injector).Storage
	registry := model.NewRegistry(&model.RegistryOptions{
		Auth:       authStorage,
		ModelsPath: modelsPath,
		BuiltIns:   nil,
	})

	return &ModelService{Registry: registry}, nil
}
