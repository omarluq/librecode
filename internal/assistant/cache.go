package assistant

import (
	"time"

	"github.com/samber/hot"
)

// ResponseCache stores deterministic local prompt responses for fast replay.
type ResponseCache struct {
	cache   *hot.HotCache[string, string]
	enabled bool
}

// NewResponseCache creates a TTL response cache.
func NewResponseCache(enabled bool, capacity int, ttl time.Duration) *ResponseCache {
	cache := hot.NewHotCache[string, string](hot.WTinyLFU, capacity).
		WithTTL(ttl).
		WithJanitor().
		Build()

	return &ResponseCache{
		cache:   cache,
		enabled: enabled,
	}
}

// Get returns a cached response when caching is enabled and the key exists.
func (cache *ResponseCache) Get(key string) (value string, found bool, err error) {
	if !cache.enabled {
		return "", false, nil
	}

	return cache.cache.Get(key)
}

// Set stores a response when caching is enabled.
func (cache *ResponseCache) Set(key, value string) {
	if cache.enabled {
		cache.cache.Set(key, value)
	}
}

// Shutdown stops the cache janitor.
func (cache *ResponseCache) Shutdown() {
	cache.cache.StopJanitor()
}
