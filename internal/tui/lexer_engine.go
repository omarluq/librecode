package tui

import (
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/samber/hot"
)

const lexerCacheCapacity = 64

// LexerEngine caches chroma lexer auto-detection results so that repeated
// renders of the same untagged code block skip the expensive O(N) scan of
// all 278+ registered lexers.
type LexerEngine struct {
	cache *hot.HotCache[string, chroma.Lexer]
}

// NewLexerEngine creates a lexer engine backed by a W-TinyLFU cache.
// The cache has no TTL because lexer detection is deterministic for
// identical text — the same input always selects the same lexer.
func NewLexerEngine() LexerEngine {
	cache := hot.NewHotCache[string, chroma.Lexer](hot.WTinyLFU, lexerCacheCapacity).Build()

	return LexerEngine{cache: cache}
}

// IteratorFor returns a token iterator for untagged code by looking up the
// cached lexer, or running full analysis on the first encounter.
func (engine *LexerEngine) IteratorFor(text string) (chroma.Iterator, bool) {
	cached, found := engine.cache.MustGet(text)
	if found {
		return tokenizeCode(cached, text)
	}

	highest := float32(0)

	var bestLexer chroma.Lexer

	for _, lexer := range lexers.GlobalLexerRegistry.Lexers {
		weight := lexer.AnalyseText(text)
		if weight > highest {
			highest = weight
			bestLexer = lexer
		}
	}

	if highest == 0 {
		return nil, false
	}

	engine.cache.Set(text, bestLexer)

	return tokenizeCode(bestLexer, text)
}
