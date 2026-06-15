package assistant

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/tool"
)

func TestToolSchemaCacheReturnsSameResult(t *testing.T) {
	t.Parallel()

	cache := newToolSchemaCache()
	registry := tool.NewRegistry(t.TempDir())

	key := toolSchemaCacheKey(registry, apiOpenAIResponses, false, false)
	cache.cache.Set(key, 1234)

	got, found := cache.cache.MustGet(key)
	assert.True(t, found)
	assert.Equal(t, 1234, got)
}

func TestToolSchemaCacheMissReturnsFalse(t *testing.T) {
	t.Parallel()

	cache := newToolSchemaCache()

	_, found := cache.cache.MustGet("nonexistent")
	assert.False(t, found)
}

func TestToolSchemaCacheKeyDifferentiatesAPI(t *testing.T) {
	t.Parallel()

	registry := tool.NewRegistry(t.TempDir())

	key1 := toolSchemaCacheKey(registry, apiOpenAICompletions, false, false)
	key2 := toolSchemaCacheKey(registry, apiOpenAIResponses, false, false)

	assert.NotEqual(t, key1, key2)
}

func TestToolSchemaCacheKeyDifferentiatesOAuth(t *testing.T) {
	t.Parallel()

	registry := tool.NewRegistry(t.TempDir())

	key1 := toolSchemaCacheKey(registry, apiAnthropicMessages, false, false)
	key2 := toolSchemaCacheKey(registry, apiAnthropicMessages, true, false)

	assert.NotEqual(t, key1, key2)
}

func TestToolSchemaCacheKeyDifferentiatesDisableTools(t *testing.T) {
	t.Parallel()

	registry := tool.NewRegistry(t.TempDir())

	enabled := toolSchemaCacheKey(registry, apiOpenAIResponses, false, false)
	disabled := toolSchemaCacheKey(registry, apiOpenAIResponses, false, true)

	assert.NotEqual(t, enabled, disabled)
	assert.Equal(t, "disabled", disabled)
}

func TestToolSchemaCacheKeySameRegistryProducesSameKey(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()

	registry1 := tool.NewRegistry(cwd)
	registry2 := tool.NewRegistry(cwd)

	key1 := toolSchemaCacheKey(registry1, apiOpenAIResponses, false, false)
	key2 := toolSchemaCacheKey(registry2, apiOpenAIResponses, false, false)

	assert.Equal(t, key1, key2)
}

func TestRuntimeEstimateToolSchemaTokensCachesResult(t *testing.T) {
	t.Parallel()

	runtime := newTestRuntimeWithSchemaCache()
	request := newSchemaEstimateRequest(t, apiOpenAIResponses, false)

	first := runtime.estimateToolSchemaTokens(request)
	assert.Positive(t, first)

	second := runtime.estimateToolSchemaTokens(request)
	assert.Equal(t, first, second)

	key := toolSchemaCacheKey(request.ToolRegistry, apiOpenAIResponses, false, false)
	cached, found := runtime.toolSchemaCache.cache.MustGet(key)
	assert.True(t, found)
	assert.Equal(t, first, cached)
}

func TestRuntimeEstimateToolSchemaTokensReturnsZeroForDisabledTools(t *testing.T) {
	t.Parallel()

	runtime := newTestRuntimeWithSchemaCache()
	request := newSchemaEstimateRequest(t, apiOpenAIResponses, true)

	assert.Zero(t, runtime.estimateToolSchemaTokens(request))
}

func TestRuntimeEstimateToolSchemaTokensReturnsZeroForNilRequest(t *testing.T) {
	t.Parallel()

	runtime := newTestRuntimeWithSchemaCache()

	assert.Zero(t, runtime.estimateToolSchemaTokens(nil))
}

func TestRuntimeEstimateToolSchemaTokensCreatesRegistryWhenNil(t *testing.T) {
	t.Parallel()

	runtime := newTestRuntimeWithSchemaCache()
	request := newSchemaEstimateRequest(t, apiOpenAIResponses, false)
	request.ToolRegistry = nil
	request.CWD = t.TempDir()

	tokens := runtime.estimateToolSchemaTokens(request)
	assert.Positive(t, tokens)
}

func newTestRuntimeWithSchemaCache() *Runtime {
	return &Runtime{
		cfg:             nil,
		sessions:        nil,
		extensions:      nil,
		cache:           nil,
		events:          nil,
		models:          nil,
		client:          nil,
		logger:          nil,
		skillsCache:     nil,
		toolSchemaCache: newToolSchemaCache(),
	}
}

func newSchemaEstimateRequest(t *testing.T, api string, disableTools bool) *CompletionRequest {
	t.Helper()

	return &CompletionRequest{
		OnEvent:           nil,
		OnProviderObserve: nil,
		OnProviderRequest: nil,
		ToolRegistry:      tool.NewRegistry(t.TempDir()),
		ExecuteTools:      nil,
		SessionID:         "",
		SystemPrompt:      "",
		ThinkingLevel:     "",
		CWD:               "",
		Auth: model.RequestAuth{
			Headers: nil,
			APIKey:  "",
			Error:   "",
			OK:      false,
		},
		Messages: nil,
		Usage: model.TokenUsage{
			Breakdown:       nil,
			TopContributors: nil,
			ContextWindow:   0,
			ContextTokens:   0,
			InputTokens:     0,
			OutputTokens:    0,
		},
		Model: model.Model{
			ThinkingLevelMap: nil,
			Headers:          nil,
			Compat:           nil,
			Provider:         "",
			ID:               "",
			Name:             "",
			API:              api,
			BaseURL:          "",
			Input:            nil,
			Cost: model.Cost{
				Input:      0,
				Output:     0,
				CacheRead:  0,
				CacheWrite: 0,
			},
			ContextWindow: 0,
			MaxTokens:     0,
			Reasoning:     false,
		},
		ProviderAttempt: 0,
		DisableTools:    disableTools,
	}
}
