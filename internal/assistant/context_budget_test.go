package assistant_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/auth"
	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/testutil"
)

func TestRuntime_ContextPreflightRejectsOversizedRequest(t *testing.T) {
	t.Parallel()

	client := &capturingCompleter{request: nil}
	runtime := newTestRuntimeWithContextWindow(t, client, 512)
	request := newRuntimePromptRequest(testRuntimeCWD, strings.Repeat("overflow ", 2600), "")

	_, err := runtime.Prompt(context.Background(), request)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "model context preflight failed")
	assert.Contains(t, err.Error(), "usable input budget")
	assert.Nil(t, client.request)
}

func TestRuntime_ContextPreflightCanBeDisabled(t *testing.T) {
	t.Parallel()

	client := &capturingCompleter{request: nil}
	runtime := newTestRuntimeWithContextWindow(t, client, 512)
	runtimeConfig := testConfig()
	runtimeConfig.Context.PreflightEnabled = false
	runtime = assistant.NewRuntimeForTest(func(opts *assistant.RuntimeTestOptions) {
		opts.Config = runtimeConfig
		opts.Sessions = runtime.SessionRepository()
		opts.Cache = assistant.NewResponseCache(false, 1, time.Minute)
		opts.Models = runtime.ModelRegistry()
		opts.Client = client
	})

	request := newRuntimePromptRequest(testRuntimeCWD, strings.Repeat("overflow ", 2600), "")

	_, err := runtime.Prompt(context.Background(), request)

	require.NoError(t, err)
	require.NotNil(t, client.request)
}

func TestRuntime_ContextUsageIncludesBudgetReserves(t *testing.T) {
	t.Parallel()

	runtime, repository := newTestRuntime(t)
	session, err := repository.CreateSession(context.Background(), testRuntimeCWD, "budget", "")
	require.NoError(t, err)

	usage, err := runtime.ContextUsage(context.Background(), session.ID, testRuntimeCWD)

	require.NoError(t, err)
	require.NotNil(t, usage.Breakdown)
	assert.Equal(t, 100_000, usage.ContextWindow)
	assert.Positive(t, usage.ContextTokens)
	assert.Positive(t, usage.Breakdown["reserve_output"])
	assert.Positive(t, usage.Breakdown["reserve_tools"])
	assert.Equal(t, 2048, usage.Breakdown["reserve_provider"])
	assert.Equal(t, 8192, usage.Breakdown["reserve_safety"])
	assert.Positive(t, usage.Breakdown["usable_input"])
}

func TestRuntime_ContextUsageHonorsExplicitZeroReserves(t *testing.T) {
	t.Parallel()

	client := &capturingCompleter{request: nil}
	runtime := newTestRuntimeWithContextWindow(t, client, 512)
	runtimeConfig := testConfig()
	runtimeConfig.Context.ProviderReserveTokens = 0
	runtimeConfig.Context.SafetyMarginTokens = 0
	runtime = assistant.NewRuntimeForTest(func(opts *assistant.RuntimeTestOptions) {
		opts.Config = runtimeConfig
		opts.Sessions = runtime.SessionRepository()
		opts.Cache = assistant.NewResponseCache(false, 1, time.Minute)
		opts.Models = runtime.ModelRegistry()
		opts.Client = client
	})

	usage, err := runtime.ContextUsage(context.Background(), "", testRuntimeCWD)

	require.NoError(t, err)
	require.NotNil(t, usage.Breakdown)
	require.Contains(t, usage.Breakdown, "reserve_provider")
	require.Contains(t, usage.Breakdown, "reserve_safety")
	assert.Equal(t, 0, usage.Breakdown["reserve_provider"])
	assert.Equal(t, 0, usage.Breakdown["reserve_safety"])
}

func TestRuntime_ContextUsageUsesDefaultOutputReserveWhenModelMaxTokensIsLarge(t *testing.T) {
	t.Parallel()

	client := &capturingCompleter{request: nil}
	runtime := newTestRuntimeWithContextWindowAndMaxTokens(t, client, 272_000, 128_000)

	usage, err := runtime.ContextUsage(context.Background(), "", testRuntimeCWD)

	require.NoError(t, err)
	assert.Equal(t, 16_384, usage.Breakdown["reserve_output"])
}

func TestRuntime_ContextUsageUsesExplicitOutputReserve(t *testing.T) {
	t.Parallel()

	client := &capturingCompleter{request: nil}
	runtime := newTestRuntimeWithContextWindowAndMaxTokens(t, client, 100_000, 128_000)
	runtimeConfig := testConfig()
	runtimeConfig.Context.OutputReserveTokens = 1234
	runtime = assistant.NewRuntimeForTest(func(opts *assistant.RuntimeTestOptions) {
		opts.Config = runtimeConfig
		opts.Sessions = runtime.SessionRepository()
		opts.Cache = assistant.NewResponseCache(false, 1, time.Minute)
		opts.Models = runtime.ModelRegistry()
		opts.Client = client
	})

	usage, err := runtime.ContextUsage(context.Background(), "", testRuntimeCWD)

	require.NoError(t, err)
	assert.Equal(t, 1234, usage.Breakdown["reserve_output"])
}

func TestLoadRejectsNegativeContextBudget(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.Context = config.ContextConfig{
		OutputReserveTokens:   0,
		ProviderReserveTokens: 0,
		SafetyMarginTokens:    -1,
		PreflightEnabled:      true,
	}

	err := cfg.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "context.safety_margin_tokens cannot be negative")
}

func newTestRuntimeWithContextWindow(
	t *testing.T,
	client assistant.Completer,
	contextWindow int,
) *assistant.Runtime {
	t.Helper()

	return newTestRuntimeWithContextWindowAndMaxTokens(t, client, contextWindow, 0)
}

func newTestRuntimeWithContextWindowAndMaxTokens(
	t *testing.T,
	client assistant.Completer,
	contextWindow int,
	maxTokens int,
) *assistant.Runtime {
	t.Helper()

	storage := testutil.NewAuthStorage(t, map[string]auth.Credential{
		testRuntimeProvider: testProviderCredential(),
	})

	registry := model.NewRegistry(&model.RegistryOptions{
		ConfigReader: nil,
		Auth:         storage,
		ModelsPath:   "",
		BuiltIns:     []model.Model{testRuntimeModelWithContextWindowAndMaxTokens(contextWindow, maxTokens)},
		Discovery:    disabledModelDiscovery(),
	})
	runtimeConfig := testConfig()
	manager := extension.NewManager(nil)
	t.Cleanup(manager.Shutdown)

	cache := assistant.NewResponseCache(true, 32, time.Minute)
	t.Cleanup(cache.Shutdown)
	_, repository := newTestRuntime(t)
	runtime := assistant.NewRuntimeForTest(func(opts *assistant.RuntimeTestOptions) {
		opts.Config = runtimeConfig
		opts.Sessions = repository
		opts.Extensions = manager
		opts.Cache = cache
		opts.Models = registry
		opts.Client = client
	})

	return runtime
}

func testRuntimeModelWithContextWindowAndMaxTokens(contextWindow, maxTokens int) model.Model {
	return model.Model{
		ThinkingLevelMap: nil,
		Headers:          nil,
		Compat:           nil,
		Provider:         testRuntimeProvider,
		ID:               testRuntimeModel,
		Name:             testRuntimeModel,
		API:              "openai-completions",
		BaseURL:          "https://example.invalid/v1",
		Input:            []model.InputMode{model.InputText},
		Cost:             model.Cost{Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0},
		ContextWindow:    contextWindow,
		MaxTokens:        maxTokens,
		Reasoning:        false,
	}
}
