package model

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/samber/oops"
)

// CachedDiscoveryOptions configures cached model discovery.
type CachedDiscoveryOptions struct {
	Client       *http.Client
	CachePath    string
	SourceURL    string
	CacheTTL     time.Duration
	FetchTimeout time.Duration
	Enabled      bool
}

// DiscoverModelsCached fetches model metadata with a stale-if-fetch-fails disk cache.
func DiscoverModelsCached(ctx context.Context, options CachedDiscoveryOptions) ([]Model, error) {
	if !options.Enabled {
		return []Model{}, nil
	}
	if cached, ok := readFreshDiscoveryCache(options.CachePath, options.CacheTTL); ok {
		return ParseDiscoveredModels(cached)
	}
	content, err := fetchDiscoveryCatalog(ctx, DiscoveryOptions{
		Client:       options.Client,
		CachePath:    "",
		SourceURL:    options.SourceURL,
		CacheTTL:     0,
		FetchTimeout: options.FetchTimeout,
		Enabled:      true,
	})
	if err != nil {
		if cached, ok := readDiscoveryCache(options.CachePath); ok {
			models, parseErr := ParseDiscoveredModels(cached)
			if parseErr == nil {
				return models, nil
			}
		}

		return []Model{}, err
	}
	if err := writeDiscoveryCache(options.CachePath, content); err != nil {
		return []Model{}, err
	}

	return ParseDiscoveredModels(content)
}

func readFreshDiscoveryCache(path string, ttl time.Duration) ([]byte, bool) {
	if path == "" || ttl == 0 {
		return nil, false
	}
	stat, err := os.Stat(path)
	if err != nil || time.Since(stat.ModTime()) > ttl {
		return nil, false
	}

	return readDiscoveryCache(path)
}

func readDiscoveryCache(path string) ([]byte, bool) {
	if path == "" {
		return nil, false
	}
	content, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, false
	}

	return content, true
}

func writeDiscoveryCache(path string, content []byte) error {
	if path == "" {
		return nil
	}
	cleanPath := filepath.Clean(path)
	if err := os.MkdirAll(filepath.Dir(cleanPath), 0o700); err != nil {
		return oops.In("model").Code("model_discovery_cache_dir").Wrapf(err, "create model discovery cache directory")
	}
	if err := os.WriteFile(cleanPath, content, 0o600); err != nil {
		return oops.In("model").Code("model_discovery_cache_write").Wrapf(err, "write model discovery cache")
	}

	return nil
}
