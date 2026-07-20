package agenttask

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
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

			_, err := NewRuntimeRunner(testCase.runtime, testCase.catalog, testCase.sessions)
			assert.ErrorContains(t, err, "required")
		})
	}
}

func TestRuntimeRunnerRunRejectsInvalidTaskBeforePrompt(t *testing.T) {
	t.Parallel()
	db, err := sql.Open("sqlite", "file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, database.Migrate(t.Context(), db))
	sessions := database.NewSessionRepository(db)
	runner, err := NewRuntimeRunner(&assistant.Runtime{}, agent.Load(t.TempDir()), sessions)
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
}

func (completer runnerCompleter) Complete(
	_ context.Context,
	request *assistant.CompletionRequest,
) (*assistant.CompletionResult, error) {
	if completer.err != nil {
		return nil, completer.err
	}

	if request.OnEvent != nil {
		request.OnEvent(assistant.StreamEvent{
			ToolCallEvent: nil, ToolEvent: nil, Usage: nil,
			Kind: assistant.StreamEventTextDelta, Text: "answer",
		})
	}

	return &assistant.CompletionResult{
		FinishReason: llm.FinishReasonStop,
		Text:         "answer",
		Thinking:     nil,
		ToolEvents:   nil,
		Usage: model.TokenUsage{
			Breakdown: nil, TopContributors: nil, ContextWindow: 1000,
			ContextTokens: 3, InputTokens: 2, OutputTokens: 1,
		},
	}, nil
}

func TestRuntimeRunnerRunsPromptAndHandlesPromptAndEventErrors(t *testing.T) {
	t.Parallel()
	db, err := sql.Open("sqlite", "file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, database.Migrate(t.Context(), db))
	sessions := database.NewSessionRepository(db)
	session, err := sessions.CreateSession(t.Context(), t.TempDir(), childSessionName, "")
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
	definition := agent.Definition{
		SourceInfo: core.SourceInfo{Path: "", Source: "", Scope: "", Origin: "", BaseDir: ""},
		Name:       testValue, Description: "", SystemPrompt: testValue,
		Model:       agent.ModelPolicy{Provider: testValue, Model: testValue, Thinking: model.ThinkingOff},
		Permissions: agent.PermissionDeny, Tools: nil, Limits: agent.Limits{Timeout: time.Minute},
	}
	policyJSON, err := jsonMarshal(definition)
	require.NoError(t, err)

	task := emptyAgentTask()
	task.ChildSessionID = session.ID
	task.AgentName = testValue
	task.Prompt = workPrompt
	task.PolicyJSON = policyJSON
	task.Depth = 2

	newRunner := func(completer assistant.Completer) *RuntimeRunner {
		runtime := assistant.NewRuntimeForTest(func(options *assistant.RuntimeTestOptions) {
			options.Config = cfg
			options.Sessions = sessions
			options.Cache = assistant.NewResponseCache(false, 1, time.Minute)
			options.Models = models
			options.Client = completer
			options.Agents = catalog
		})
		runner, runnerErr := NewRuntimeRunner(runtime, catalog, sessions)
		require.NoError(t, runnerErr)

		return runner
	}

	runPromptScenarios(t, newRunner, task)
}

func runPromptScenarios(
	t *testing.T,
	newRunner func(assistant.Completer) *RuntimeRunner,
	task *database.AgentTaskEntity,
) {
	t.Helper()

	var kinds []string

	result, err := newRunner(runnerCompleter{err: nil}).Run(
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

	result, err = newRunner(runnerCompleter{err: errors.New("provider unavailable")}).Run(
		t.Context(), task, func(context.Context, string, any) error { return nil },
	)
	require.ErrorContains(t, err, "run agent prompt")

	var failedUsage agentUsage
	require.NoError(t, json.Unmarshal([]byte(result.UsageJSON), &failedUsage))
	assert.Equal(t, 4, failedUsage.InputTokens)

	sinkErr := errors.New("persist event")
	result, err = newRunner(runnerCompleter{err: nil}).Run(
		t.Context(), task, func(context.Context, string, any) error { return sinkErr },
	)
	require.ErrorIs(t, err, sinkErr)
	assert.Equal(t, "answer", result.Text)
	assert.JSONEq(t, `{}`, result.UsageJSON)
}

func TestRuntimeRunnerReportsSessionLoadError(t *testing.T) {
	t.Parallel()
	db, err := sql.Open("sqlite", "file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared")
	require.NoError(t, err)
	require.NoError(t, database.Migrate(t.Context(), db))
	sessions := database.NewSessionRepository(db)
	require.NoError(t, db.Close())
	runner, err := NewRuntimeRunner(&assistant.Runtime{}, agent.Load(t.TempDir()), sessions)
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
	runner := &RuntimeRunner{runtime: nil, catalog: catalog, sessions: nil}

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
		PolicyJSON: "", UsageJSON: "", Depth: 0,
	}
}

func jsonMarshal(value any) (string, error) {
	encoded, err := json.Marshal(value)

	return string(encoded), err
}
