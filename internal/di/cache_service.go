package di

import (
	"github.com/samber/do/v2"

	"github.com/omarluq/librecode/internal/agent"
)

// CacheService owns process-local hot caches.
type CacheService struct {
	Responses *agent.ResponseCache
}

// NewCacheService creates samber/hot caches from config.
func NewCacheService(injector do.Injector) (*CacheService, error) {
	cfg := do.MustInvoke[*ConfigService](injector).Get()

	return &CacheService{
		Responses: agent.NewResponseCache(cfg.Cache.Enabled, cfg.Cache.Capacity, cfg.Cache.TTL),
	}, nil
}

// Shutdown stops cache janitors.
func (service *CacheService) Shutdown() {
	service.Responses.Shutdown()
}
