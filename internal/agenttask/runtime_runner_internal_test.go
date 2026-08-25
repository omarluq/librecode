package agenttask

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite" // Register the SQLite database/sql driver used by these tests.

	"github.com/omarluq/librecode/internal/agent"
	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/auth"
	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/core"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/outputschema"
	"github.com/omarluq/librecode/internal/testutil"
	"github.com/omarluq/librecode/internal/tool"
)

const (
	generalAgent = "general"
	testValue    = "test"
	workPrompt   = "work"
)

func TestNewRuntimeRunnerRequiresDependencies(t *testing.T) {
	t.Parallel()

	catalog := agent.Load(t.TempDir())
	tasks := &database.AgentTaskRepository{}

	for _, testCase := range []struct {
		runtime  *assistant.Runtime
		catalog  *agent.Catalog
		sessions *database.SessionRepository
		name     string
	}{
		{name: "runtime", runtime: nil, catalog: catalog, sessions: &database.SessionRepository{}},
		{name: "catalog", runtime: &assistant.Runtime{}, catalog: nil, sessions: &database.SessionRepository{}},
		{name: "sessions", runtime: &assistant.Runtime{}, catalog: catalog, sessions: nil},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := NewRuntimeRunner(testCase.runtime, testCase.catalog, testCase.sessions, tasks)
			assert.ErrorContains(t, err, "required")
		})
	}
}

func TestRuntimeRunnerRunRejectsInvalidTaskBeforePrompt(t *testing.T) {
	t.Parallel()
	db := testutil.OpenMemoryDatabase(t)
	sessions := testutil.SessionRepository(t, db)
	tasks := testutil.AgentTaskRepository(t, db)
	runner, err := NewRuntimeRunner(&assistant.Runtime{}, agent.Load(t.TempDir()), sessions, tasks)
	require.NoError(t, err)

	invalidTask := emptyAgentTask()
	invalidTask.PolicyJSON = `{bad`
	result, err := runner.Run(t.Context(), invalidTask, nil)
	require.ErrorContains(t, err, "decode agent profile")
	assert.JSONEq(t, `{}`, result.UsageJSON)

	missingSessionTask := emptyAgentTask()
	missingSessionTask.PolicyJSON = `{}`
	missingSessionTask.AgentName = generalAgent
	missingSessionTask.ChildSessionID = "missing"
	result, err = runner.Run(t.Context(), missingSessionTask, nil)
	require.ErrorContains(t, err, "child session not found")
	assert.JSONEq(t, `{}`, result.UsageJSON)
}

type runnerCompleter struct {
	err error
	// streamText, when set, is emitted as text deltas before the completer
	// returns its result or error.
	streamText []string
}

func (completer runnerCompleter) Complete(
	ctx context.Context,
	request *assistant.CompletionRequest,
) (*assistant.CompletionResult, error) {
	if completer.streamText != nil && request.OnEvent != nil {
		for _, text := range completer.streamText {
			request.OnEvent(assistant.StreamEvent{
				ToolCallEvent: nil, ToolEvent: nil, Usage: nil,
				Kind: assistant.StreamEventTextDelta, Text: text,
			})
		}
	}

	if completer.err != nil {
		return nil, completer.err
	}

	baseUsage := model.TokenUsage{Provenance: model.UsageProvenance(""),
		Breakdown: nil, TopContributors: nil, ContextWindow: 1000,
		ContextTokens: 3, InputTokens: 2, OutputTokens: 1,
	}

	usage := baseUsage.WithReported()
	if request.OnProviderResponse != nil {
		request.OnProviderResponse(ctx, usage)
	}

	if request.OnEvent != nil {
		request.OnEvent(assistant.StreamEvent{
			ToolCallEvent: nil, ToolEvent: nil, Usage: nil,
			Kind: assistant.StreamEventTextDelta, Text: "answer",
		})
	}

	return &assistant.CompletionResult{Termination: llm.NewTerminationMetadata("", "", ""),
		FinishReason: llm.FinishReasonStop,
		Text:         "answer",
		Thinking:     nil,
		ToolEvents:   nil,
		Usage:        usage,
	}, nil
}

// sequenceCompleter keeps response order explicit for structured-output retries.
type sequenceCompleter struct {
	responses []string
	calls     int
}

func (completer *sequenceCompleter) Complete(
	ctx context.Context,
	request *assistant.CompletionRequest,
) (*assistant.CompletionResult, error) {
	completer.calls++

	text := completer.responses[completer.calls-1]
	usageValue := model.TokenUsage{
		Provenance: "", Breakdown: nil, TopContributors: nil,
		ContextWindow: 1000, ContextTokens: 3, InputTokens: 2, OutputTokens: 1,
	}

	usage := usageValue.WithReported()
	if request.OnProviderResponse != nil {
		request.OnProviderResponse(ctx, usage)
	}

	return &assistant.CompletionResult{
		Termination: llm.NewTerminationMetadata("", "", ""), FinishReason: llm.FinishReasonStop,
		Text: text, Thinking: nil, ToolEvents: nil, Usage: usage,
	}, nil
}

type countingErrorCompleter struct {
	err   error
	calls int
}

func (completer *countingErrorCompleter) Complete(
	context.Context,
	*assistant.CompletionRequest,
) (*assistant.CompletionResult, error) {
	completer.calls++

	return nil, completer.err
}

type runnerFixture struct {
	task      *database.AgentTaskEntity
	tasks     *database.AgentTaskRepository
	newRunner func(assistant.Completer) *RuntimeRunner
}

func newRunnerFixture(t *testing.T, depth int) runnerFixture {
	t.Helper()

	db := testutil.OpenMemoryDatabase(t)
	sessions := testutil.SessionRepository(t, db)
	tasks := testutil.AgentTaskRepository(t, db)
	parent := testutil.CreateSession(t, sessions, "parent")
	session, err := sessions.CreateSession(t.Context(), parent.CWD, childSessionName, parent.ID)
	require.NoError(t, err)

	cfg := config.Load("").MustGet()
	models := model.NewRegistry(&model.RegistryOptions{
		ConfigReader: nil,
		Auth: testutil.NewAuthStorage(t, map[string]auth.Credential{
			testValue: {
				OAuth: nil, Type: auth.CredentialTypeAPIKey, Key: "secret", Access: "", Refresh: "",
				AccountID: "", Expires: 0, ExpiresAt: 0,
			},
		}),
		ModelsPath: "", BuiltIns: nil, Discovery: model.DiscoveryOptions{
			Client: nil, CachePath: "", SourceURL: "", CacheTTL: 0,
			FetchTimeout: 0, Enabled: false,
		},
	})
	catalog := agent.Load(t.TempDir())
	policyJSON, err := jsonMarshal(runnerTestDefinition())
	require.NoError(t, err)

	task := emptyAgentTask()
	task.Task.OwnerSessionID = parent.ID
	task.ChildSessionID = session.ID
	task.AgentName = testValue
	task.Prompt = workPrompt
	task.PolicyJSON = policyJSON
	task.Depth = depth

	newRunner := func(completer assistant.Completer) *RuntimeRunner {
		runtime := assistant.NewRuntimeForTest(func(options *assistant.RuntimeTestOptions) {
			options.Config = cfg
			options.Sessions = sessions
			options.Cache = assistant.NewResponseCache(false, 1, time.Minute)
			t.Cleanup(options.Cache.Shutdown)
			options.Models = models
			options.Client = completer
			options.Agents = catalog
		})
		runner, runnerErr := NewRuntimeRunner(runtime, catalog, sessions, tasks)
		require.NoError(t, runnerErr)

		return runner
	}

	return runnerFixture{task: task, tasks: tasks, newRunner: newRunner}
}

func TestRuntimeRunnerStructuredOutputAttempts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		wantText      string
		wantError     string
		responses     []string
		wantCalls     int
		wantCompleted int
	}{
		{name: "first attempt success", responses: []string{`{"value":"ok"}`},
			wantText: `{"value":"ok"}`, wantError: "", wantCalls: 1, wantCompleted: 1},
		{name: "repair success", responses: []string{`{"value":1}`, `{"value":"fixed"}`},
			wantText: `{"value":"fixed"}`, wantError: "", wantCalls: 2, wantCompleted: 2},
		{name: "three invalid attempts", responses: []string{`{}`, `{"value":1}`, `null`},
			wantText: "", wantError: "validate structured output", wantCalls: 3, wantCompleted: 3},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			fixture := newStructuredRunnerFixture(t, 0)
			completer := &sequenceCompleter{responses: test.responses, calls: 0}

			result, err := fixture.newRunner(completer).Run(t.Context(), fixture.task, nil)
			if test.wantError == "" {
				require.NoError(t, err)
				assert.JSONEq(t, test.wantText, result.Text)
			} else {
				require.ErrorContains(t, err, test.wantError)

				var schemaErr *outputschema.Error
				require.ErrorAs(t, err, &schemaErr,
					"exhaustion must retain the structured-output classification")
			}

			assert.Equal(t, test.wantCalls, completer.calls)
			loaded, found, loadErr := fixture.tasks.Get(t.Context(), fixture.task.Task.ID)
			require.NoError(t, loadErr)
			require.True(t, found)
			assert.Equal(t, test.wantCalls, loaded.OutputAttemptsReserved)
			assert.Equal(t, test.wantCompleted, loaded.OutputAttemptsCompleted)

			var usage model.UsageTotals
			require.NoError(t, json.Unmarshal([]byte(loaded.UsageJSON), &usage))
			assert.Equal(t, int64(2*test.wantCompleted), usage.InputTokens)
			assert.Equal(t, int64(test.wantCompleted), usage.OutputTokens)
		})
	}
}

func TestRuntimeRunnerDoesNotRepairProviderOrCancellationFailures(t *testing.T) {
	t.Parallel()

	for _, runErr := range []error{errors.New("provider unavailable"), context.Canceled} {
		fixture := newStructuredRunnerFixture(t, 0)
		completer := &countingErrorCompleter{err: runErr, calls: 0}
		_, err := fixture.newRunner(completer).Run(t.Context(), fixture.task, nil)
		require.ErrorIs(t, err, runErr)
		assert.Equal(t, 1, completer.calls)

		loaded, found, loadErr := fixture.tasks.Get(t.Context(), fixture.task.Task.ID)
		require.NoError(t, loadErr)
		require.True(t, found)
		assert.Equal(t, 1, loaded.OutputAttemptsReserved)
		assert.Zero(t, loaded.OutputAttemptsCompleted)
	}
}

func TestRuntimeRunnerReservedSlotRecoveryAndExhaustion(t *testing.T) {
	t.Parallel()

	recovered := newStructuredRunnerFixture(t, 1)
	completer := &sequenceCompleter{responses: []string{`{"value":"recovered"}`}, calls: 0}
	result, err := recovered.newRunner(completer).Run(t.Context(), recovered.task, nil)
	require.NoError(t, err)
	assert.JSONEq(t, `{"value":"recovered"}`, result.Text)
	loaded, found, err := recovered.tasks.Get(t.Context(), recovered.task.Task.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, 2, loaded.OutputAttemptsReserved)
	assert.Equal(t, 1, loaded.OutputAttemptsCompleted)

	exhausted := newStructuredRunnerFixture(t, maxOutputAttempts)
	unused := &sequenceCompleter{responses: []string{`{"value":"unused"}`}, calls: 0}
	_, err = exhausted.newRunner(unused).Run(t.Context(), exhausted.task, nil)
	require.ErrorContains(t, err, "attempt budget exhausted")
	assert.Zero(t, unused.calls)
}

func newStructuredRunnerFixture(t *testing.T, reserved int) runnerFixture {
	t.Helper()

	fixture := newRunnerFixture(t, 1)
	contract, err := outputschema.Admit(`{
		"type":"object",
		"required":["value"],
		"properties":{"value":{"type":"string"}},
		"additionalProperties":false
	}`)
	require.NoError(t, err)

	fixture.task.OutputSchemaJSON = string(contract.Canonical)
	fixture.task.OutputSchemaDigest = contract.Digest
	fixture.task.OutputAttemptsReserved = reserved

	created, err := fixture.tasks.Create(t.Context(), fixture.task)
	require.NoError(t, err)
	claimed, err := fixture.tasks.Tasks().ClaimQueued(t.Context(), &database.TaskClaim{
		TaskID: created.Task.ID, LeaseOwner: "worker", EventKind: "task_started",
		LeaseExpiresAt: time.Now().Add(time.Minute),
	})
	require.NoError(t, err)
	require.True(t, claimed)

	var found bool

	fixture.task, found, err = fixture.tasks.Get(t.Context(), created.Task.ID)
	require.NoError(t, err)
	require.True(t, found)

	return fixture
}

func TestRuntimeRunnerRunsPromptAndHandlesPromptAndEventErrors(t *testing.T) {
	t.Parallel()

	fixture := newRunnerFixture(t, 2)
	runPromptScenarios(t, fixture.newRunner, fixture.task)
}

func runPromptScenarios(
	t *testing.T,
	newRunner func(assistant.Completer) *RuntimeRunner,
	task *database.AgentTaskEntity,
) {
	t.Helper()

	var kinds []string

	result, err := newRunner(runnerCompleter{err: nil, streamText: nil}).Run(
		t.Context(), task, func(_ context.Context, kind string, _ any) error {
			kinds = append(kinds, kind)

			return nil
		},
	)
	require.NoError(t, err)
	assert.Equal(t, "answer", result.Text)

	var usage model.TokenUsage
	require.NoError(t, json.Unmarshal([]byte(result.UsageJSON), &usage))
	assert.Equal(t, 2, usage.InputTokens)
	assert.Equal(t, 1, usage.OutputTokens)
	assert.Contains(t, kinds, string(assistant.StreamEventTextDelta))
	assert.Contains(t, kinds, string(assistant.StreamEventUsageTotal))

	result, err = newRunner(runnerCompleter{err: errors.New("provider unavailable"), streamText: nil}).Run(
		t.Context(), task, func(context.Context, string, any) error { return nil },
	)
	require.ErrorContains(t, err, "run agent prompt")

	var failedUsage model.UsageTotals
	require.NoError(t, json.Unmarshal([]byte(result.UsageJSON), &failedUsage))
	assert.Zero(t, failedUsage.InputTokens)
	assert.False(t, failedUsage.Reported)

	sinkErr := errors.New("persist event")
	result, err = newRunner(runnerCompleter{err: nil, streamText: nil}).Run(
		t.Context(), task, func(context.Context, string, any) error { return sinkErr },
	)
	require.ErrorIs(t, err, sinkErr)
	assert.Equal(t, "answer", result.Text)

	var sinkFailureUsage model.UsageTotals
	require.NoError(t, json.Unmarshal([]byte(result.UsageJSON), &sinkFailureUsage))
	assert.Equal(t, int64(2), sinkFailureUsage.InputTokens)
	assert.True(t, sinkFailureUsage.Reported)
}

func TestRuntimeRunnerSurfacesPartialTextWhenPromptFails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err  error
		name string
	}{
		{name: "context cancellation", err: context.Canceled},
		{name: "provider failure", err: errors.New("provider unavailable")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			fixture := newRunnerFixture(t, 1)
			completer := runnerCompleter{err: test.err, streamText: []string{"partial ", "findings"}}
			runner := fixture.newRunner(completer)

			nopSink := func(context.Context, string, any) error { return nil }
			result, err := runner.Run(t.Context(), fixture.task, nopSink)
			require.ErrorIs(t, err, test.err)
			assert.Equal(t, "partial findings", result.Text)

			var usage model.UsageTotals
			require.NoError(t, json.Unmarshal([]byte(result.UsageJSON), &usage))
			assert.Zero(t, usage.InputTokens)
			assert.False(t, usage.Reported)
		})
	}
}

func TestPartialTextAccumulatesConcurrently(t *testing.T) {
	t.Parallel()

	var partial partialText
	partial.observe(assistant.StreamEvent{
		ToolCallEvent: nil, ToolEvent: nil, Usage: nil,
		Kind: assistant.StreamEventTextDelta, Text: "one",
	})
	partial.observe(assistant.StreamEvent{
		ToolCallEvent: nil, ToolEvent: nil, Usage: nil,
		Kind: assistant.StreamEventThinkingDelta, Text: "ignored",
	})

	const writers = 16

	const writesPerWriter = 50

	var writersWG sync.WaitGroup
	writersWG.Add(writers)

	for range writers {
		go func() {
			defer writersWG.Done()

			for range writesPerWriter {
				partial.observe(assistant.StreamEvent{
					ToolCallEvent: nil, ToolEvent: nil, Usage: nil,
					Kind: assistant.StreamEventTextDelta, Text: "x",
				})
			}
		}()
	}

	writersWG.Wait()

	assert.Len(t, partial.text(), len("one")+writers*writesPerWriter)
	assert.True(t, strings.HasPrefix(partial.text(), "one"))
}

func TestRuntimeRunnerReportsSessionLoadError(t *testing.T) {
	t.Parallel()
	db := testutil.OpenMemoryDatabase(t)
	sessions := testutil.SessionRepository(t, db)
	tasks := testutil.AgentTaskRepository(t, db)
	require.NoError(t, db.Close())
	runner, err := NewRuntimeRunner(&assistant.Runtime{}, agent.Load(t.TempDir()), sessions, tasks)
	require.NoError(t, err)

	task := emptyAgentTask()
	task.AgentName = generalAgent
	task.PolicyJSON = `{}`
	task.ChildSessionID = childSessionName
	result, err := runner.Run(t.Context(), task, nil)
	require.ErrorContains(t, err, "load child session")
	assert.JSONEq(t, `{}`, result.UsageJSON)
}

func TestRuntimeRunnerResolvesPersistedAndCatalogDefinitions(t *testing.T) {
	t.Parallel()
	catalog := agent.Load(t.TempDir())
	runner := &RuntimeRunner{runtime: nil, catalog: catalog, sessions: nil, tasks: nil}

	persisted := agent.Definition{
		SourceInfo: emptySourceInfo(), Name: "snapshot", Description: "", SystemPrompt: "persisted",
		Model:       agent.ModelPolicy{Provider: "p", Model: "m", Thinking: model.ThinkingHigh},
		Permissions: agent.PermissionDeny, Tools: []tool.Name{tool.NameRead},
		Limits: agent.Limits{Timeout: time.Minute},
	}

	raw, err := jsonMarshal(persisted)
	require.NoError(t, err)

	persistedTask := emptyAgentTask()
	persistedTask.PolicyJSON = raw
	definition, err := runner.taskDefinition(persistedTask)
	require.NoError(t, err)
	assert.Equal(t, persisted, *definition)

	catalogTask := emptyAgentTask()
	catalogTask.PolicyJSON = `{}`
	catalogTask.AgentName = " GENERAL "
	definition, err = runner.taskDefinition(catalogTask)
	require.NoError(t, err)
	assert.Equal(t, "general", definition.Name)

	invalidTask := emptyAgentTask()
	invalidTask.PolicyJSON = `{bad`
	_, err = runner.taskDefinition(invalidTask)
	require.ErrorContains(t, err, "decode agent profile")

	missingTask := emptyAgentTask()
	missingTask.PolicyJSON = `{}`
	missingTask.AgentName = "missing"
	_, err = runner.taskDefinition(missingTask)
	require.ErrorContains(t, err, "agent definition not found")
}

func TestProfileFromDefinitionIsBackgroundAndDeterministic(t *testing.T) {
	t.Parallel()

	definition := &agent.Definition{
		SourceInfo: emptySourceInfo(), Name: "review", Description: "", SystemPrompt: "review carefully",
		Model:       agent.ModelPolicy{Provider: "openai", Model: "gpt", Thinking: model.ThinkingMedium},
		Permissions: agent.PermissionAsk, Tools: []tool.Name{tool.NameRead, tool.NameGrep},
		Limits: agent.Limits{Timeout: 0},
	}
	profile := profileFromDefinition(definition, 3)
	assert.Equal(t, assistant.ExecutionAgentTask, profile.Kind)
	assert.Equal(t, "review", profile.AgentName)
	assert.Equal(t, 3, profile.Depth)
	assert.False(t, profile.EnableSkills)
	assert.False(t, profile.EnableExtensions)
	assert.Equal(t, definition.Tools, profile.Tools)
	assert.Equal(t, definition.Model.Thinking, profile.ThinkingLevel)
}

func runnerTestDefinition() agent.Definition {
	return agent.Definition{
		SourceInfo: emptySourceInfo(), Name: testValue, Description: "", SystemPrompt: testValue,
		Model:       agent.ModelPolicy{Provider: testValue, Model: testValue, Thinking: model.ThinkingOff},
		Permissions: agent.PermissionDeny, Tools: nil, Limits: agent.Limits{Timeout: time.Minute},
	}
}

func emptySourceInfo() core.SourceInfo {
	return core.SourceInfo{Path: "", Source: "", Scope: "", Origin: "", BaseDir: ""}
}

func emptyAgentTask() *database.AgentTaskEntity {
	return &database.AgentTaskEntity{
		Task: database.TaskEntity{
			CreatedAt: time.Time{}, StartedAt: nil, FinishedAt: nil, UpdatedAt: time.Time{}, LeaseExpiresAt: nil,
			ID: "", Kind: "", ParentTaskID: "", OwnerSessionID: "", ConcurrencyKey: "", LeaseOwner: "",
			State: "", Result: "", ErrorCode: "", ErrorMessage: "",
		},
		ChildSessionID: "", AgentName: "", Prompt: "", Model: "", Provider: "",
		PolicyJSON: "", UsageJSON: "", OutputSchemaJSON: "", OutputSchemaDigest: "",
		OutputAttemptsReserved: 0, OutputAttemptsCompleted: 0, OutputValidationSummary: "", Depth: 0,
	}
}

func jsonMarshal(value any) (string, error) {
	encoded, err := json.Marshal(value)

	return string(encoded), err
}
