package di

import (
	"path/filepath"

	"github.com/samber/do/v2"

	"github.com/omarluq/librecode/internal/agent"
	"github.com/omarluq/librecode/internal/assistant"
)

// AssistantService exposes the assistant runtime.
type AssistantService struct {
	Runtime *assistant.Runtime
	Agents  *agent.Catalog
}

// NewAssistantService wires the assistant runtime.
func NewAssistantService(injector do.Injector) (*AssistantService, error) {
	configService, err := do.Invoke[*ConfigService](injector)
	if err != nil {
		return nil, err
	}

	databaseService, err := do.Invoke[*DatabaseService](injector)
	if err != nil {
		return nil, err
	}

	extensionService, err := do.Invoke[*ExtensionService](injector)
	if err != nil {
		return nil, err
	}

	cache, err := do.Invoke[*CacheService](injector)
	if err != nil {
		return nil, err
	}

	models, err := do.Invoke[*ModelService](injector)
	if err != nil {
		return nil, err
	}

	loggerService, err := do.Invoke[*LoggerService](injector)
	if err != nil {
		return nil, err
	}

	skills, err := do.Invoke[*SkillsService](injector)
	if err != nil {
		return nil, err
	}

	cwd, err := filepath.Abs(".")
	if err != nil {
		return nil, serviceError(err, "resolve agent working directory")
	}

	agents := agent.Load(cwd)

	return &AssistantService{
		Runtime: assistant.NewRuntime(&assistant.RuntimeOptions{
			Config:      configService.Get(),
			Sessions:    databaseService.Sessions,
			Extensions:  extensionService.Manager,
			Cache:       cache.Responses,
			Models:      models.Registry,
			Client:      nil,
			Logger:      loggerService.SlogLogger,
			SkillsCache: skills.Cache,
			Agents:      agents,
		}),
		Agents: agents,
	}, nil
}
