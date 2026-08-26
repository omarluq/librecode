package assistant

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/contextwindow"
	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/model"
)

func TestClassifyCompletionRuntimeSignals(t *testing.T) {
	t.Parallel()

	structuredOverflow := oops.In("provider").Code("context_length_exceeded").Errorf("too large")
	tests := []struct {
		name    string
		request *CompletionRequest
		result  *CompletionResult
		err     error
		want    ResponseClass
	}{
		{name: "explicit overflow", request: recoveryRequest("", 0), result: nil,
			err: structuredOverflow, want: ResponseExplicitContextOverflow},
		{name: "unstructured error is not replayable", request: recoveryRequest("", 0), result: nil,
			err: errors.New("context window exceeded"), want: ResponseProviderError},
		{name: "anthropic silent overflow", request: recoveryRequest(anthropicProvider, 0), err: nil,
			result: recoveryResult("model_context_window_exceeded", 0), want: ResponseMetadataContextOverflow},
		{name: "anthropic premature length", request: recoveryRequest(anthropicProvider, 1000), err: nil,
			result: recoveryResult("max_tokens", 100), want: ResponseMetadataContextOverflow},
		{name: "anthropic output exhaustion", request: recoveryRequest(anthropicProvider, 1000), err: nil,
			result: recoveryResult("max_tokens", 990), want: ResponseOutputLengthTruncation},
		{name: "openai short length remains truncation", request: recoveryRequest("openai", 1000), err: nil,
			result: recoveryResult("", 100), want: ResponseOutputLengthTruncation},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := classifyCompletion(testCase.request, testCase.result, testCase.err)
			assert.Equal(t, testCase.want, got.Class)
		})
	}
}

func TestDecideOverflowRecoveryRejectsRuntimeReplayHazards(t *testing.T) {
	t.Parallel()

	identity := emptyRequestIdentity()
	identity.LogicalRequestID, identity.Provider, identity.Model = "run", anthropicProvider, "model"
	identity.LineageParentEntryID, identity.ProviderAttempt, identity.CompactionGeneration = "parent", 1, 2

	classification := ResponseClassification{Class: ResponseMetadataContextOverflow,
		FinishReason: llm.FinishReasonUnknown, IncompleteReason: "", ContextOverflowSignal: "",
		OutputLimit: 0, ReportedOutputTokens: 0}

	staleModel := identity
	staleModel.Model = "new-model"
	staleLineage := identity
	staleLineage.LineageParentEntryID = "new-parent"

	tests := []struct {
		name   string
		want   string
		active RequestIdentity
		replay ReplayState
	}{
		{name: "stale model", active: staleModel,
			replay: ReplayState{RecoveryConsumed: false, ToolDispatchStarted: false, LineageAdvanced: false},
			want:   "stale_identity"},
		{name: "stale lineage", active: staleLineage,
			replay: ReplayState{RecoveryConsumed: false, ToolDispatchStarted: false, LineageAdvanced: true},
			want:   "stale_identity"},
		{name: "tool side effects", active: identity,
			replay: ReplayState{RecoveryConsumed: false, ToolDispatchStarted: true, LineageAdvanced: false},
			want:   "side_effects_started"},
		{name: "recovery consumed", active: identity,
			replay: ReplayState{RecoveryConsumed: true, ToolDispatchStarted: false, LineageAdvanced: false},
			want:   "retry_consumed"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			decision := DecideOverflowRecovery(OverflowRecoveryDecisionInput{
				Classification: classification, Identity: identity, ActiveIdentity: testCase.active,
				Replay: testCase.replay, HasCompactionCandidate: true,
			})
			assert.False(t, decision.Recover)
			assert.Equal(t, testCase.want, decision.Refusal)
		})
	}
}

func TestProviderOverflowRecoveryStateReservesOnce(t *testing.T) {
	t.Parallel()

	const competitors = 32

	var (
		state   providerOverflowRecoveryState
		winners atomic.Int32
		wait    sync.WaitGroup
	)

	wait.Add(competitors)

	for range competitors {
		go func() {
			defer wait.Done()

			if state.reserve() {
				winners.Add(1)
			}
		}()
	}

	wait.Wait()
	assert.Equal(t, int32(1), winners.Load())
	assert.True(t, state.consumed.Load())
}

func TestActiveRequestIdentityUsesCurrentRuntimeState(t *testing.T) {
	t.Parallel()

	selected := model.Model{
		ThinkingLevelMap: nil, Headers: nil, Compat: nil, Provider: "test-provider", ID: "test-model",
		Name: "test-model", API: apiOpenAICompletions, BaseURL: "https://example.invalid/v1",
		Input: []model.InputMode{model.InputText}, Cost: model.Cost{
			Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0,
		}, ContextWindow: 64_000, MaxTokens: 4_096, Reasoning: false,
	}
	registry := model.NewRegistryContext(t.Context(), &model.RegistryOptions{
		ConfigReader: nil, Auth: nil, ModelsPath: "", BuiltIns: []model.Model{selected},
		Discovery: model.DiscoveryOptions{
			Client: nil, CachePath: "", SourceURL: "", CacheTTL: 0, FetchTimeout: 0, Enabled: false,
		},
	})
	cfg := testRuntimeConfig()
	cfg.Assistant.Provider = selected.Provider
	cfg.Assistant.Model = selected.ID
	runtime := NewRuntimeForTest(func(options *RuntimeTestOptions) {
		options.Config = cfg
		options.Models = registry
	})

	lineage := newPromptLineage("run")
	lineage.activeParentEntryID = "current-parent"
	lineage.generation = 3
	request := newZeroCompletionRequest(model.RequestAuth{Headers: nil, APIKey: "", Error: "", OK: true})
	request.Identity = RequestIdentity{
		LogicalRequestID: "run", Provider: selected.Provider, Model: selected.ID,
		LineageParentEntryID: "old-parent", ProviderAttempt: 2, CompactionGeneration: 2, RecoveryAttempt: 0,
	}
	input := &providerOverflowRecoveryInput{
		onRetry: nil,
		preparation: &completionRequestPreparationInput{
			selectedModel: nil, onEvent: nil, lineage: lineage, auth: nil, sessionID: "", cwd: "", prompt: "",
		},
		build: &contextRequestBuild{Context: nil, Request: request, Budget: contextwindow.Budget{
			InputTokens: 0, ContextWindow: 0, UsableInput: 0, OutputReserve: 0, ToolSchemaReserve: 0,
			ProviderReserve: 0, SafetyMargin: 0,
		}},
		compactionEntry: nil, recovery: newProviderOverflowRecoveryState(),
	}
	input.recovery.authoritativeProviderAttempt.Store(2)

	active, ok := runtime.activeRequestIdentity(request, input)
	assert.True(t, ok)
	assert.Equal(t, "current-parent", active.LineageParentEntryID)
	assert.Equal(t, uint64(3), active.CompactionGeneration)
	assert.NotEqual(t, request.Identity, active)

	request.Identity.LineageParentEntryID = active.LineageParentEntryID
	request.Identity.CompactionGeneration = active.CompactionGeneration
	request.Identity.ProviderAttempt = 1
	assert.False(t, runtime.requestIdentityIsActive(request, input), "stale provider attempt must be rejected")

	request.Identity.ProviderAttempt = 2

	input.recovery.recoveryAttemptAuthorized.Store(true)
	assert.False(t, runtime.requestIdentityIsActive(request, input), "stale recovery attempt must be rejected")

	request.Identity.RecoveryAttempt = 1
	assert.True(t, runtime.requestIdentityIsActive(request, input))
}

func recoveryRequest(provider string, maxTokens int) *CompletionRequest {
	request := newZeroCompletionRequest(model.RequestAuth{Headers: nil, APIKey: "", Error: "", OK: false})
	request.Model.Provider = provider
	request.MaxTokens = maxTokens

	return request
}

func recoveryResult(providerReason string, outputTokens int) *CompletionResult {
	result := &CompletionResult{
		FinishReason: llm.FinishReasonLength,
		Termination:  llm.NewTerminationMetadata("", providerReason, ""),
		Text:         "",
		Thinking:     nil,
		ToolEvents:   nil,
		Usage:        model.EmptyTokenUsage(),
	}
	result.Usage.OutputTokens = outputTokens

	return result
}
