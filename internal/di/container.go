// Package di wires the application runtime dependency graph.
package di

import (
	"context"

	"github.com/samber/do/v2"
	"github.com/samber/oops"
)

// Container wraps the root injector used by the CLI runtime.
type Container struct {
	injector *do.RootScope
}

// NewContainer builds the root injector for the CLI runtime.
func NewContainer(configPath string, overrides ConfigOverrides) (*Container, error) {
	injector := do.New()
	do.ProvideNamedValue(injector, ConfigPathKey, configPath)
	do.ProvideNamedValue(injector, ConfigOverridesKey, overrides)
	RegisterServices(injector)

	if _, err := do.Invoke[*ConfigService](injector); err != nil {
		return nil, oops.
			In("di").
			Code("container_init").
			Wrapf(err, "initialize container")
	}

	return &Container{injector: injector}, nil
}

// ShutdownWithContext stops all registered services using the given context.
func (c *Container) ShutdownWithContext(ctx context.Context) *do.ShutdownReport {
	return c.injector.ShutdownWithContext(ctx)
}

// ConfigService resolves the configuration service.
func (c *Container) ConfigService() *ConfigService {
	return do.MustInvoke[*ConfigService](c.injector)
}

// AuthService resolves the auth service.
func (c *Container) AuthService() *AuthService {
	return do.MustInvoke[*AuthService](c.injector)
}

// DatabaseService resolves the database service.
func (c *Container) DatabaseService() *DatabaseService {
	return do.MustInvoke[*DatabaseService](c.injector)
}

// EventService resolves the event service.
func (c *Container) EventService() *EventService {
	return do.MustInvoke[*EventService](c.injector)
}

// ExtensionService resolves the extension service.
func (c *Container) ExtensionService() *ExtensionService {
	return do.MustInvoke[*ExtensionService](c.injector)
}

// ModelService resolves the model service.
func (c *Container) ModelService() *ModelService {
	return do.MustInvoke[*ModelService](c.injector)
}

// AssistantService resolves the assistant service.
func (c *Container) AssistantService() *AssistantService {
	return do.MustInvoke[*AssistantService](c.injector)
}

// ToolService resolves the tool service.
func (c *Container) ToolService() *ToolService {
	return do.MustInvoke[*ToolService](c.injector)
}

// SkillsService resolves the skills cache service.
func (c *Container) SkillsService() *SkillsService {
	return do.MustInvoke[*SkillsService](c.injector)
}
