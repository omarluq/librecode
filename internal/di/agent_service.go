package di

import (
	"github.com/samber/do/v2"

	"github.com/omarluq/librecode/internal/agent"
)

// AgentService exposes the local agent runtime.
type AgentService struct {
	Runtime *agent.Runtime
}

// NewAgentService wires the local agent runtime.
func NewAgentService(injector do.Injector) (*AgentService, error) {
	cfg := do.MustInvoke[*ConfigService](injector).Get()
	database := do.MustInvoke[*DatabaseService](injector)
	plugins := do.MustInvoke[*PluginService](injector)
	cache := do.MustInvoke[*CacheService](injector)
	logger := do.MustInvoke[*LoggerService](injector).SlogLogger

	return &AgentService{
		Runtime: agent.NewRuntime(cfg, database.Store, plugins.Manager, cache.Responses, logger),
	}, nil
}
