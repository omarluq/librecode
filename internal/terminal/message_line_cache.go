package terminal

import "github.com/omarluq/librecode/internal/terminal/rendertext"

const messageCacheWarmBatchSize = 16

type messageLineCache struct {
	items     []cachedRenderedMessage
	prefixes  []int
	state     messageLineCacheState
	warm      bool
	queued    bool
	warmIndex int
}

func (cache *messageLineCache) ensure(app *App, width, targetLength int) {
	state := app.currentLineCacheState(width)
	if cache.state != state {
		cache.items = nil
		cache.prefixes = nil
		cache.state = state
		cache.warm = false
		cache.queued = false
		cache.warmIndex = 0
	}
	if len(cache.items) > targetLength {
		cache.items = cache.items[:targetLength]
		cache.prefixes = nil
		cache.warm = false
		cache.warmIndex = min(cache.warmIndex, targetLength)
	}
	for len(cache.items) < targetLength {
		cache.items = append(cache.items, emptyCachedRenderedMessage())
	}
	if len(cache.prefixes) != len(cache.items)+1 {
		cache.prefixes = nil
	}
}

func (cache *messageLineCache) reset() {
	cache.items = nil
	cache.prefixes = nil
	cache.warmIndex = 0
	cache.warm = false
	cache.queued = false
}

func (cache *messageLineCache) appendInvalidation() {
	cache.warmIndex = 0
	cache.warm = false
	cache.queued = false
}

func (cache *messageLineCache) truncate(length int) {
	if len(cache.items) > length {
		cache.items = cache.items[:length]
	}
	cache.prefixes = nil
	cache.warmIndex = 0
	cache.warm = false
	cache.queued = false
}

func (cache *messageLineCache) lines(app *App, width, index int) []rendertext.Line {
	cache.ensure(app, width, len(app.transcript.History))
	if index < len(cache.items) && cache.items[index].Valid {
		return cache.items[index].Lines
	}
	lines := app.renderMessage(width, app.transcript.History[index])
	if index >= len(cache.items) {
		return lines
	}
	cache.items[index] = cachedRenderedMessage{Lines: lines, Valid: true}
	cache.prefixes = nil

	return lines
}

func (cache *messageLineCache) rebuildPrefixes(app *App, width int) {
	cache.ensure(app, width, len(app.transcript.History))
	prefixes := make([]int, len(cache.items)+1)
	for index := range cache.items {
		if !cache.items[index].Valid {
			cache.items[index] = cachedRenderedMessage{
				Lines: app.renderMessage(width, app.transcript.History[index]),
				Valid: true,
			}
		}
		prefixes[index+1] = prefixes[index] + len(cache.items[index].Lines)
	}
	cache.prefixes = prefixes
}

func (cache *messageLineCache) rebuildPrefixesFromCache() {
	prefixes := make([]int, len(cache.items)+1)
	for index := range cache.items {
		prefixes[index+1] = prefixes[index] + len(cache.items[index].Lines)
	}
	cache.prefixes = prefixes
}

func (cache *messageLineCache) warmStep(app *App) bool {
	if cache.warm || app.toolsExpanded || len(app.transcript.History) == 0 || app.transcript.LastMaxRows <= 0 {
		return false
	}
	width := app.currentLineCacheStateWidth()
	cache.ensure(app, width, len(app.transcript.History))
	start := min(max(0, cache.warmIndex), len(app.transcript.History))
	end := min(len(app.transcript.History), start+messageCacheWarmBatchSize)
	if end <= start {
		return false
	}
	for index := start; index < end; index++ {
		if !cache.items[index].Valid {
			cache.items[index] = cachedRenderedMessage{
				Lines: app.renderMessage(width, app.transcript.History[index]),
				Valid: true,
			}
		}
	}
	cache.warmIndex = end
	if end < len(app.transcript.History) {
		return true
	}
	cache.rebuildPrefixesFromCache()
	cache.warm = true

	return true
}

func emptyMessageLineCacheState() messageLineCacheState {
	var state messageLineCacheState

	return state
}

func (app *App) ensureLineCache(
	width int,
	targetLength int,
	cache *[]cachedRenderedMessage,
	cacheState *messageLineCacheState,
) {
	state := app.currentLineCacheState(width)
	if *cacheState != state {
		*cache = nil
		*cacheState = state
	}
	if len(*cache) > targetLength {
		*cache = (*cache)[:targetLength]
	}
	for len(*cache) < targetLength {
		*cache = append(*cache, emptyCachedRenderedMessage())
	}
}

func emptyMessageLineCache() messageLineCache {
	return messageLineCache{
		items:     nil,
		prefixes:  nil,
		state:     emptyMessageLineCacheState(),
		warm:      false,
		queued:    false,
		warmIndex: 0,
	}
}
