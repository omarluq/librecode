package assistant

import (
	"slices"
	"strconv"
	"strings"

	"github.com/samber/hot"
	"github.com/samber/lo"

	"github.com/omarluq/librecode/internal/tool"
)

const (
	// toolSchemaCacheCapacity limits how many distinct schema estimates are
	// cached. In practice there are at most a handful of keys (one per
	// API type × OAuth mode combination).
	toolSchemaCacheCapacity = 8
)

// toolSchemaCache memoizes tool schema token estimates using samber/hot so
// that repeated budget calculations within and across prompts do not
// re-marshal tool definitions and re-estimate tokens.
//
// The estimate depends only on the tool set (built-in names are fixed,
// extension names change on reload), the provider API, the Anthropic OAuth
// mode, and the DisableTools flag.
type toolSchemaCache struct {
	cache *hot.HotCache[string, int]
}

func newToolSchemaCache() *toolSchemaCache {
	return &toolSchemaCache{
		cache: hot.NewHotCache[string, int](hot.WTinyLFU, toolSchemaCacheCapacity).Build(),
	}
}

// toolSchemaCacheKey builds a cache key from every factor that influences
// the serialized tool schema: the sorted tool names, API type, OAuth mode,
// and DisableTools flag. Two registries with the same key produce identical
// token estimates.
func toolSchemaCacheKey(registry *tool.Registry, api string, oauth, disableTools bool) string {
	if disableTools {
		return "disabled"
	}

	names := lo.Map(registry.Definitions(), func(definition tool.Definition, _ int) string {
		return string(definition.Name)
	})
	slices.Sort(names)

	return strings.Join(
		[]string{api, strconv.FormatBool(oauth), strings.Join(names, ",")},
		"\x00",
	)
}

// estimateToolSchemaTokens returns the cached token estimate for the tool
// schema in the given request, computing and storing it on first access.
// The computation marshals API-specific tool definitions to JSON and
// estimates tokens from the resulting string.
func (runtime *Runtime) estimateToolSchemaTokens(request *CompletionRequest) int {
	if request == nil || request.DisableTools {
		return 0
	}

	registry := request.ToolRegistry
	if registry == nil {
		registry = tool.NewRegistry(request.CWD)
		request.ToolRegistry = registry
	}

	oauth := requestUsesAnthropicOAuth(request)
	key := toolSchemaCacheKey(registry, request.Model.API, oauth, request.DisableTools)

	if tokens, found := runtime.toolSchemaCache.cache.MustGet(key); found {
		return tokens
	}

	tokens := computeToolSchemaTokens(request)

	runtime.toolSchemaCache.cache.Set(key, tokens)

	return tokens
}
