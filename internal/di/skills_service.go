package di

import (
	"github.com/samber/do/v2"

	"github.com/omarluq/librecode/internal/core"
)

// SkillsService owns the process-level skills cache.
type SkillsService struct {
	Cache *core.SkillsCache
}

// NewSkillsService creates a skills cache that eliminates redundant filesystem
// scans on every prompt.
func NewSkillsService(_ do.Injector) (*SkillsService, error) {
	return &SkillsService{
		Cache: core.NewSkillsCache(),
	}, nil
}

// Shutdown stops the cache's background file watcher.
func (service *SkillsService) Shutdown() {
	service.Cache.Close()
}
