package agent

import (
	"time"

	"github.com/samber/hot"
)

// ResponseCache stores deterministic local prompt responses for fast replay.
type ResponseCache struct {
	enabled bool
	cache   *hot.HotCache[string, string]
}

// NewResponseCache creates a TTL cache backed by samber/hot.
func NewResponseCache(enabled bool, capacity int, ttl time.Duration) *ResponseCache {
	cache := hot.NewHotCache[string, string](hot.WTinyLFU, capacity).
		WithTTL(ttl).
		WithJanitor().
		Build()

	return &ResponseCache{
		enabled: enabled,
		cache:   cache,
	}
}

// Get returns a cached response when caching is enabled and the key exists.
func (cache *ResponseCache) Get(key string) (string, bool, error) {
	if !cache.enabled {
		return "", false, nil
	}

	return cache.cache.Get(key)
}

// Set stores a response when caching is enabled.
func (cache *ResponseCache) Set(key string, value string) {
	if cache.enabled {
		cache.cache.Set(key, value)
	}
}

// Shutdown stops the cache janitor.
func (cache *ResponseCache) Shutdown() {
	cache.cache.StopJanitor()
}
