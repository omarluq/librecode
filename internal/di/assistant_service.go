package di

import (
	"github.com/samber/do/v2"

	"github.com/omarluq/librecode/internal/assistant"
)

// AssistantService exposes the assistant runtime.
type AssistantService struct {
	Runtime *assistant.Runtime
}

// NewAssistantService wires the assistant runtime.
func NewAssistantService(injector do.Injector) (*AssistantService, error) {
	cfg := do.MustInvoke[*ConfigService](injector).Get()
	databaseService := do.MustInvoke[*DatabaseService](injector)
	extensionService := do.MustInvoke[*ExtensionService](injector)
	cache := do.MustInvoke[*CacheService](injector)
	events := do.MustInvoke[*EventService](injector)
	models := do.MustInvoke[*ModelService](injector)
	logger := do.MustInvoke[*LoggerService](injector).SlogLogger
	skills := do.MustInvoke[*SkillsService](injector)

	return &AssistantService{
		Runtime: assistant.NewRuntime(&assistant.RuntimeOptions{
			Config:      cfg,
			Sessions:    databaseService.Sessions,
			Extensions:  extensionService.Manager,
			Cache:       cache.Responses,
			Events:      events.Bus,
			Models:      models.Registry,
			Client:      nil,
			Logger:      logger,
			SkillsCache: skills.Cache,
		}),
	}, nil
}
