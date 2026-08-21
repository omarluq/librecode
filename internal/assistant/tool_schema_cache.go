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
// The estimate depends on the complete provider-visible definitions, the
// provider API, the Anthropic OAuth mode, and the DisableTools flag.
type toolSchemaCache struct {
	cache *hot.HotCache[string, int]
}

func newToolSchemaCache() *toolSchemaCache {
	return &toolSchemaCache{
		cache: hot.NewHotCache[string, int](hot.WTinyLFU, toolSchemaCacheCapacity).Build(),
	}
}

// toolSchemaCacheKey builds a cache key from every factor that influences the
// serialized provider schema, including definition content so profile
// availability changes cannot reuse a stale estimate.
func toolSchemaCacheKey(registry *tool.Registry, api string, oauth, disableTools bool) string {
	if disableTools {
		return "disabled"
	}

	definitions := lo.Map(registry.Definitions(), func(definition tool.Definition, _ int) string {
		return strings.Join([]string{
			string(definition.Name), definition.Description, string(definition.Schema.RawMessage()),
			strconv.FormatBool(definition.ReadOnly),
		}, "\x1f")
	})
	slices.Sort(definitions)

	return strings.Join(
		[]string{api, strconv.FormatBool(oauth), strings.Join(definitions, "\x1e")},
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
