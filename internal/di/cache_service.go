package di

import (
	"github.com/samber/do/v2"

	"github.com/omarluq/librecode/internal/assistant"
)

// CacheService owns process-local response caches.
type CacheService struct {
	Responses *assistant.ResponseCache
}

// NewCacheService creates response caches from config.
func NewCacheService(injector do.Injector) (*CacheService, error) {
	cfg := do.MustInvoke[*ConfigService](injector).Get()

	return &CacheService{
		Responses: assistant.NewResponseCache(cfg.Cache.Enabled, cfg.Cache.Capacity, cfg.Cache.TTL),
	}, nil
}

// Shutdown stops cache janitors.
func (service *CacheService) Shutdown() {
	service.Responses.Shutdown()
}
