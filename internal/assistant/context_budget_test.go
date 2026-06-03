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
	"github.com/omarluq/librecode/internal/event"
	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/model"
)

func TestRuntime_ContextPreflightRejectsOversizedRequest(t *testing.T) {
	t.Parallel()

	client := &capturingCompletionClient{request: nil}
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

	client := &capturingCompletionClient{request: nil}
	runtime := newTestRuntimeWithContextWindow(t, client, 512)
	runtimeConfig := testConfig()
	runtimeConfig.Context.PreflightEnabled = false
	runtime = assistant.NewRuntime(
		runtimeConfig,
		runtime.SessionRepository(),
		nil,
		assistant.NewResponseCache(false, 1, time.Minute),
		runtime.EventBus(),
		runtime.ModelRegistry(),
		client,
		nil,
	)

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
	assert.Greater(t, usage.ContextTokens, 0)
	assert.Greater(t, usage.Breakdown["reserve_output"], 0)
	assert.Greater(t, usage.Breakdown["reserve_tools"], 0)
	assert.Equal(t, 2048, usage.Breakdown["reserve_provider"])
	assert.Equal(t, 8192, usage.Breakdown["reserve_safety"])
	assert.Greater(t, usage.Breakdown["usable_input"], 0)
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
	client assistant.CompletionClient,
	contextWindow int,
) *assistant.Runtime {
	t.Helper()

	storage, err := auth.NewInMemoryStorage(context.Background(), map[string]auth.Credential{
		testRuntimeProvider: testProviderCredential(),
	})
	require.NoError(t, err)
	registry := model.NewRegistry(&model.RegistryOptions{
		ConfigSource: nil,
		Auth:         storage,
		ModelsPath:   "",
		BuiltIns:     []model.Model{testRuntimeModelWithContextWindow(contextWindow)},
	})
	runtimeConfig := testConfig()
	manager := extension.NewManager(nil)
	t.Cleanup(manager.Shutdown)
	cache := assistant.NewResponseCache(true, 32, time.Minute)
	t.Cleanup(cache.Shutdown)
	_, repository := newTestRuntime(t)
	runtime := assistant.NewRuntime(
		runtimeConfig,
		repository,
		manager,
		cache,
		event.NewBus(nil),
		registry,
		client,
		nil,
	)

	return runtime
}

func testRuntimeModelWithContextWindow(contextWindow int) model.Model {
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
		MaxTokens:        0,
		Reasoning:        false,
	}
}
