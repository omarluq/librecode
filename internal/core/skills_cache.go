package core

import (
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/samber/hot"
)

const (
	// skillsCacheCapacity limits how many working directories are cached.
	skillsCacheCapacity = 16
	// skillsCacheTTL is a backstop expiration. fsnotify handles real-time
	// invalidation; the TTL ensures stale entries are eventually evicted even
	// if watcher events are missed.
	skillsCacheTTL = 1 * time.Hour
	// skillsCacheDebounce coalesces bursts of fsnotify events (e.g. a git checkout
	// touching dozens of files) into a single cache purge so the next prompt only
	// re-reads from disk once instead of once per file event.
	skillsCacheDebounce = 500 * time.Millisecond
)

func loadCachedResources(cwd string) LoadSkillsResult {
	result := LoadSkills(cwd, nil, true)
	result.AgentInstructions = LoadAgentInstructions(cwd)

	return result
}

// SkillsCache memoizes LoadSkills results per working directory using samber/hot.
// Cached entries are invalidated automatically when underlying files change
// via fsnotify so that new or edited skills are picked up without a restart.
// Burst events (e.g. git checkout) are debounced so a burst triggers a single
// purge rather than one per file.
// If the watcher cannot be created the cache still functions as simple memoization.
type SkillsCache struct {
	cache    *hot.HotCache[string, LoadSkillsResult]
	watcher  *fsnotify.Watcher
	purgeTim *time.Timer
	done     chan struct{}
	purgeMu  sync.Mutex
	wg       sync.WaitGroup
	once     sync.Once
}

// NewSkillsCache creates a skills cache backed by samber/hot with an fsnotify watcher.
func NewSkillsCache() *SkillsCache {
	return newSkillsCache(skillsCacheTTL)
}

func newSkillsCache(ttl time.Duration) *SkillsCache {
	cache := hot.NewHotCache[string, LoadSkillsResult](hot.WTinyLFU, skillsCacheCapacity).
		WithTTL(ttl).
		WithLoaders(func(cwds []string) (map[string]LoadSkillsResult, error) {
			results := make(map[string]LoadSkillsResult, len(cwds))

			for _, cwd := range cwds {
				results[cwd] = loadCachedResources(cwd)
			}

			return results, nil
		}).
		WithJanitor().
		Build()

	skills := &SkillsCache{
		cache:    cache,
		watcher:  nil,
		done:     make(chan struct{}),
		purgeMu:  sync.Mutex{},
		wg:       sync.WaitGroup{},
		once:     sync.Once{},
		purgeTim: nil,
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Debug("skills cache watcher unavailable, degrading to memoization", slog.Any("error", err))

		return skills
	}

	skills.watcher = watcher
	skills.wg.Add(1)

	go skills.watch()

	return skills
}

// Get returns the cached skills for cwd, loading from disk on first access
// or after a file system change has invalidated the cache.
func (c *SkillsCache) Get(cwd string) LoadSkillsResult {
	result, _, err := c.cache.Get(cwd)
	if err != nil {
		slog.Debug("skills cache loader error, falling back to direct load", slog.Any("error", err))

		return loadCachedResources(cwd)
	}

	c.watchSkillDirs(cwd, result.Skills)
	c.watchAgentInstructionDirs(cwd)

	return result
}

// Close stops the background watcher goroutine and cache janitor.
// It is safe to call multiple times and when the watcher was never started.
func (c *SkillsCache) Close() {
	c.once.Do(func() {
		close(c.done)
		c.cancelPurge()

		if c.watcher != nil {
			if err := c.watcher.Close(); err != nil {
				slog.Debug("skills cache watcher close error", slog.Any("error", err))
			}
		}

		c.wg.Wait()
		c.cache.StopJanitor()
	})
}

func (c *SkillsCache) watch() {
	defer c.wg.Done()

	for {
		select {
		case <-c.done:
			return
		case _, ok := <-c.watcher.Events:
			if !ok {
				return
			}

			c.schedulePurge()
		case _, ok := <-c.watcher.Errors:
			if !ok {
				return
			}

			c.schedulePurge()
		}
	}
}

// schedulePurge coalesces bursts of fsnotify events into a single cache purge.
// Each event resets the debounce timer, so a rapid burst (e.g. git checkout touching
// dozens of files) triggers exactly one purge after activity settles.
func (c *SkillsCache) schedulePurge() {
	c.purgeMu.Lock()
	defer c.purgeMu.Unlock()

	if c.purgeTim != nil {
		c.purgeTim.Stop()
	}

	c.purgeTim = time.AfterFunc(skillsCacheDebounce, func() {
		c.purgeMu.Lock()
		c.purgeTim = nil
		c.purgeMu.Unlock()

		c.cache.Purge()
	})
}

// cancelPurge stops any pending debounced purge. Called during shutdown to
// ensure no timer fires after Close returns.
func (c *SkillsCache) cancelPurge() {
	c.purgeMu.Lock()
	defer c.purgeMu.Unlock()

	if c.purgeTim != nil {
		c.purgeTim.Stop()
		c.purgeTim = nil
	}
}

func (c *SkillsCache) watchSkillDirs(cwd string, skills []Skill) {
	if c.watcher == nil {
		return
	}

	watched := make(map[string]struct{})

	for _, path := range defaultSkillPaths(cwd) {
		dir := nearestExistingDir(path)
		if _, seen := watched[dir]; seen {
			continue
		}

		watched[dir] = struct{}{}

		c.addWatch(dir)
	}

	for index := range skills {
		dir := filepath.Clean(skills[index].BaseDir)
		if _, seen := watched[dir]; seen {
			continue
		}

		watched[dir] = struct{}{}

		c.addWatch(dir)
	}
}

func (c *SkillsCache) watchAgentInstructionDirs(cwd string) {
	if c.watcher == nil {
		return
	}

	for _, dir := range agentInstructionDirs(cwd) {
		c.addWatch(nearestExistingDir(dir))
	}

	if home, err := LibrecodeHome(); err == nil {
		c.addWatch(nearestExistingDir(home))
	}
}

func (c *SkillsCache) addWatch(dir string) {
	if err := c.watcher.Add(dir); err != nil {
		slog.Debug("skills cache watcher add error", slog.String("dir", dir), slog.Any("error", err))
	}
}

// nearestExistingDir walks up from path until it finds a directory that exists.
// This ensures fsnotify can watch a parent directory when a skill root doesn't
// exist yet, so that creating it later triggers cache invalidation.
// If nothing exists up to the filesystem root, the original path is returned
// so the caller still attempts the watch (fsnotify will report an error but
// that is handled gracefully).
func nearestExistingDir(path string) string {
	dir := filepath.Clean(path)

	for !resourcePathExists(dir) {
		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Clean(path)
		}

		dir = parent
	}

	return dir
}
