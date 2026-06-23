package model

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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

	staleContent, hasStaleCache := readDiscoveryCache(options.CachePath)

	fetched, err := fetchDiscoveryCatalogConditional(
		ctx,
		discoveryOptionsFromCached(options),
		discoveryCachedETag(options.CachePath, hasStaleCache),
	)
	if err != nil {
		return staleDiscoveredModelsOrError(staleContent, hasStaleCache, err)
	}

	if fetched.NotModified {
		return discoveredModelsFromNotModifiedCache(options.CachePath, staleContent, hasStaleCache, fetched.ETag)
	}

	if err := writeDiscoveryCache(options.CachePath, fetched.Content); err != nil {
		return []Model{}, err
	}

	if err := writeDiscoveryCacheETag(options.CachePath, fetched.ETag); err != nil {
		return []Model{}, err
	}

	return ParseDiscoveredModels(fetched.Content)
}

func discoveryOptionsFromCached(options CachedDiscoveryOptions) DiscoveryOptions {
	return DiscoveryOptions{
		Client:       options.Client,
		CachePath:    "",
		SourceURL:    options.SourceURL,
		CacheTTL:     0,
		FetchTimeout: options.FetchTimeout,
		Enabled:      true,
	}
}

func discoveryCachedETag(cachePath string, hasCache bool) string {
	if !hasCache {
		return ""
	}

	etag, ok := readDiscoveryCacheETag(cachePath)
	if !ok {
		return ""
	}

	return etag
}

func staleDiscoveredModelsOrError(content []byte, ok bool, err error) ([]Model, error) {
	if ok {
		models, parseErr := ParseDiscoveredModels(content)
		if parseErr == nil {
			return models, nil
		}
	}

	return []Model{}, err
}

func discoveredModelsFromNotModifiedCache(cachePath string, content []byte, ok bool, etag string) ([]Model, error) {
	if !ok {
		return []Model{}, oops.In("model").Code("model_discovery_cache_miss").Errorf(
			"model discovery cache was not available for not modified response",
		)
	}

	models, err := ParseDiscoveredModels(content)
	if err != nil {
		return []Model{}, err
	}

	if err := touchDiscoveryCache(cachePath); err != nil {
		return []Model{}, err
	}

	if etag == "" {
		return models, nil
	}

	if err := writeDiscoveryCacheETag(cachePath, etag); err != nil {
		return []Model{}, err
	}

	return models, nil
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

func touchDiscoveryCache(path string) error {
	if path == "" {
		return nil
	}

	now := time.Now()
	if err := os.Chtimes(filepath.Clean(path), now, now); err != nil {
		return oops.In("model").Code("model_discovery_cache_touch").Wrapf(err, "refresh model discovery cache")
	}

	return nil
}

func readDiscoveryCacheETag(cachePath string) (string, bool) {
	content, ok := readDiscoveryCache(discoveryCacheETagPath(cachePath))
	if !ok {
		return "", false
	}

	etag := normalizeDiscoveryETag(string(content))

	return etag, etag != ""
}

func writeDiscoveryCacheETag(cachePath, value string) error {
	path := discoveryCacheETagPath(cachePath)
	if path == "" {
		return nil
	}

	etag := normalizeDiscoveryETag(value)
	if etag == "" {
		if err := os.Remove(filepath.Clean(path)); err != nil && !os.IsNotExist(err) {
			return oops.In("model").Code("model_discovery_etag_remove").Wrapf(
				err,
				"remove model discovery cache etag",
			)
		}

		return nil
	}

	return writeDiscoveryCache(path, []byte(etag+"\n"))
}

func discoveryCacheETagPath(cachePath string) string {
	if cachePath == "" {
		return ""
	}

	return filepath.Clean(cachePath) + ".etag"
}

func normalizeDiscoveryETag(value string) string {
	etag := strings.TrimSpace(value)
	if etag == "" || strings.HasPrefix(etag, "W/") || strings.Contains(etag, ",") {
		return ""
	}

	etag = strings.Trim(etag, `"`)

	return strings.TrimSpace(etag)
}

const (
	modelCacheDirMode  = 0o700
	modelCacheFileMode = 0o600
)

func writeDiscoveryCache(path string, content []byte) error {
	if path == "" {
		return nil
	}

	cleanPath := filepath.Clean(path)
	if err := os.MkdirAll(filepath.Dir(cleanPath), modelCacheDirMode); err != nil {
		return oops.In("model").Code("model_discovery_cache_dir").Wrapf(err, "create model discovery cache directory")
	}

	if err := os.WriteFile(cleanPath, content, modelCacheFileMode); err != nil {
		return oops.In("model").Code("model_discovery_cache_write").Wrapf(err, "write model discovery cache")
	}

	return nil
}
