package assistant

import (
	"context"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/contextwindow"
	"github.com/omarluq/librecode/internal/model"
)

type staleOverflowCompleter struct {
	mutate func()
	calls  int
}

func (client *staleOverflowCompleter) Complete(
	_ context.Context,
	_ *CompletionRequest,
) (*CompletionResult, error) {
	client.calls++
	client.mutate()

	return nil, oopsContextWindowError()
}

func TestRuntimeOverflowRejectsStaleResponseAfterRuntimeStateChange(t *testing.T) {
	t.Parallel()

	t.Run("model switch", func(t *testing.T) {
		t.Parallel()
		testRuntimeOverflowRejectsStaleResponse(t, func(runtime *Runtime, _ *promptLineage) {
			runtime.profile.Model = "replacement-model"
		})
	})
	t.Run("lineage advance", func(t *testing.T) {
		t.Parallel()
		testRuntimeOverflowRejectsStaleResponse(t, func(_ *Runtime, lineage *promptLineage) {
			lineage.generation++
		})
	})
}

func testRuntimeOverflowRejectsStaleResponse(
	t *testing.T,
	mutate func(runtime *Runtime, lineage *promptLineage),
) {
	t.Helper()

	models := []model.Model{
		failureSafetyModel("original-model"),
		failureSafetyModel("replacement-model"),
	}
	registry := model.NewRegistry(&model.RegistryOptions{
		ConfigReader: nil, Auth: nil, ModelsPath: "", BuiltIns: models,
		Discovery: model.DiscoveryOptions{
			Client: nil, CachePath: "", SourceURL: "", CacheTTL: 0, FetchTimeout: 0, Enabled: false,
		},
	})
	cfg := testRuntimeConfig()
	cfg.Assistant.Provider = models[0].Provider
	cfg.Assistant.Model = models[0].ID
	lineage := newPromptLineage("run")
	client := &staleOverflowCompleter{mutate: nil, calls: 0}
	runtime := NewRuntimeForTest(func(options *RuntimeTestOptions) {
		options.Config = cfg
		options.Models = registry
		options.Client = client
	})
	client.mutate = func() { mutate(runtime, lineage) }

	auth := model.RequestAuth{Headers: nil, APIKey: compactionTestOrigin, Error: "", OK: true}
	request := runtime.modelCompletionRequest(&modelCompletionRequestInput{
		selectedModel: &models[0], registry: nil, onEvent: nil, lineage: lineage,
		sessionID: "session", systemPrompt: "", cwd: "", auth: auth,
		messages: nil, usage: model.EmptyTokenUsage(),
	})
	build := &contextRequestBuild{Context: nil, Request: request, Budget: contextwindow.Budget{
		InputTokens: 0, ContextWindow: models[0].ContextWindow, UsableInput: models[0].ContextWindow,
		OutputReserve: 0, ToolSchemaReserve: 0, ProviderReserve: 0, SafetyMargin: 0,
	}}
	input := &providerOverflowRecoveryInput{
		onRetry: nil,
		preparation: &completionRequestPreparationInput{
			selectedModel: &models[0], onEvent: nil, lineage: lineage, auth: &auth,
			sessionID: "session", cwd: "", prompt: "prompt",
		},
		build: build, compactionEntry: nil, recovery: newProviderOverflowRecoveryState(),
	}

	_, _, result, err := runtime.completeWithProviderOverflowRecovery(t.Context(), input)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, 1, client.calls, "a stale overflow response must not compact or replay")
}

func failureSafetyModel(id string) model.Model {
	return model.Model{
		ThinkingLevelMap: nil, Headers: nil, Compat: nil,
		Provider: "failure-safety-provider", ID: id, Name: id,
		API: apiOpenAICompletions, BaseURL: "https://example.invalid/v1",
		Input:         []model.InputMode{model.InputText},
		Cost:          model.Cost{Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0},
		ContextWindow: 64_000, MaxTokens: 4_096, Reasoning: false,
	}
}

func oopsContextWindowError() error {
	return oops.In("assistant_test").Code("context_window_exceeded").Errorf("provider context window exceeded")
}
