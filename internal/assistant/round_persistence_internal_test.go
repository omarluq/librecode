package assistant

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite" // Register the SQLite driver used by these tests.

	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/llmconv"
	"github.com/omarluq/librecode/internal/testutil"
)

const testPersistenceStart = "start"

type roundPersistenceTestHarness struct {
	runtime    *Runtime
	repository *database.SessionRepository
	sessionID  string
	rootID     string
}

func newRoundPersistenceTestHarness(t *testing.T, title string) roundPersistenceTestHarness {
	t.Helper()

	ctx := t.Context()
	connection, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	connection.SetMaxOpenConns(1)
	require.NoError(t, database.Migrate(ctx, connection))

	repository := testutil.SessionRepository(t, connection)
	runtime := NewRuntimeForTest(func(options *RuntimeTestOptions) {
		options.Config = testRuntimeConfig()
		options.Sessions = repository
	})
	session, err := repository.CreateSession(ctx, t.TempDir(), title, "")
	require.NoError(t, err)
	root, err := repository.AppendMessage(ctx, session.ID, nil, &database.MessageEntity{
		Timestamp: repositoryNow(), Role: database.RoleUser, Content: testPersistenceStart,
		Provider: "", Model: "", Parts: nil,
	})
	require.NoError(t, err)

	return roundPersistenceTestHarness{
		runtime: runtime, repository: repository, sessionID: session.ID, rootID: root.ID,
	}
}

func TestRuntime_PersistCompletedRoundPreservesOrderAndUsage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	harness := newRoundPersistenceTestHarness(t, "checkpoint")

	baseUsage := llm.Usage{
		Provenance: "",
		Breakdown:  nil, TopContributors: nil,
		ContextWindow: 128_000, ContextTokens: 32_000, InputTokens: 11, OutputTokens: 7,
	}
	usage := baseUsage.WithReported()
	lineage := newPromptLineage(harness.rootID)
	entry, err := harness.runtime.persistCompletedRoundWithPersistence(
		ctx, harness.sessionID, lineage, &llm.CompletedRound{
			Assistant: llm.Message{Metadata: nil, Role: llm.RoleAssistant, Content: []llm.Part{
				{Metadata: nil, ToolCall: nil, ToolResult: nil, Type: llm.PartReasoning,
					Text: "reason", Data: "", MIMEType: ""},
				llm.TextPart("working"),
			}},
			ToolResults: []llm.ToolResult{{
				Metadata:   map[string]any{toolParentCallIDMetadataKey: "parent", toolSequenceMetadataKey: 2},
				ToolCallID: "call-1", ArgumentsJSON: `{"path":"README.md"}`, Name: "read", Error: "",
				Content: []llm.Part{llm.TextPart("contents")}, IsError: false,
			}},
			FinishReason: llm.FinishReasonToolCalls,
			Usage:        usage,
		},
	)
	require.NoError(t, err)
	assert.Equal(t, entry.ID, lineage.activeParentEntryID)

	messages, err := harness.repository.Messages(ctx, harness.sessionID)
	require.NoError(t, err)
	require.Len(t, messages, 4)
	assert.Equal(t, []database.Role{
		database.RoleUser,
		database.RoleThinking,
		database.RoleToolResult,
		database.RoleAssistant,
	}, []database.Role{messages[0].Role, messages[1].Role, messages[2].Role, messages[3].Role})
	assert.Equal(t, "working", messages[3].Content)
	assert.Contains(t, messages[2].Content, "call_id: call-1")
	assert.Contains(t, messages[2].Content, "parent_call_id: parent")
	assert.Contains(t, messages[2].Content, "sequence: 2")

	contextEntity, err := harness.repository.BuildContext(ctx, harness.sessionID, entry.ID)
	require.NoError(t, err)
	require.NotNil(t, contextEntity.UsageAnchor)

	expectedUsage := llmconv.UsageToModel(&usage)
	assert.Equal(t, expectedUsage.InputTokens, contextEntity.UsageAnchor.Usage.InputTokens)
	assert.Equal(t, expectedUsage.OutputTokens, contextEntity.UsageAnchor.Usage.OutputTokens)
}

func TestRuntime_PersistCompletedRoundCoalescesHighDeltaThinkingAtBoundary(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	harness := newRoundPersistenceTestHarness(t, "joined thinking")

	const deltaCount = 4_096

	deltas := make([]string, deltaCount)
	for index := range deltas {
		deltas[index] = "x"
	}

	joinedThinking := "  first  line\n" + strings.Join(deltas, "") + "\nlast line  "
	wantThinking := strings.TrimSpace(joinedThinking)

	lineage := newPromptLineage(harness.rootID)
	entry, err := harness.runtime.persistCompletedRoundWithPersistence(
		ctx, harness.sessionID, lineage, &llm.CompletedRound{
			Assistant: llm.Message{Metadata: nil, Role: llm.RoleAssistant, Content: []llm.Part{
				{
					Metadata: nil, ToolCall: nil, ToolResult: nil, Type: llm.PartReasoning,
					Text: joinedThinking, Data: "", MIMEType: "",
				},
				llm.TextPart("finished"),
			}},
			ToolResults: []llm.ToolResult{{
				Metadata: nil, ToolCallID: "call-scale", ArgumentsJSON: `{}`, Name: "read", Error: "",
				Content: []llm.Part{llm.TextPart("result")}, IsError: false,
			}},
			FinishReason: llm.FinishReasonToolCalls,
			Usage:        llm.EmptyUsage(),
		},
	)
	require.NoError(t, err)

	messages, err := harness.repository.Messages(ctx, harness.sessionID)
	require.NoError(t, err)
	require.Len(t, messages, 4)
	assert.Equal(t, []database.Role{
		database.RoleUser,
		database.RoleThinking,
		database.RoleToolResult,
		database.RoleAssistant,
	}, []database.Role{messages[0].Role, messages[1].Role, messages[2].Role, messages[3].Role})
	assert.Equal(t, wantThinking, messages[1].Content)
	assert.Equal(t, "finished", messages[3].Content)

	thinkingCount := 0

	for index := range messages {
		if messages[index].Role == database.RoleThinking {
			thinkingCount++
		}
	}

	assert.Equal(t, 1, thinkingCount, "persistence must scale with completed rounds, not transport deltas")

	contextEntity, err := harness.repository.BuildContext(ctx, harness.sessionID, entry.ID)
	require.NoError(t, err)
	require.Len(t, contextEntity.Messages, 2)
	assert.Equal(t, []database.Role{
		database.RoleUser,
		database.RoleAssistant,
	}, []database.Role{contextEntity.Messages[0].Role, contextEntity.Messages[1].Role})
}

func TestRuntime_PersistCompletedRoundRejectsNilInput(t *testing.T) {
	t.Parallel()

	runtime := NewRuntimeForTest(nil)
	lineage := newPromptLineage("entry")

	_, err := runtime.persistCompletedRoundWithPersistence(context.Background(), "session", lineage, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "completed round is required")
	assert.Nil(t, responseBundleFromCompletedRound(nil))
}

func testRuntimeConfig() *config.Config {
	return &config.Config{
		App: config.AppConfig{
			Name: "librecode", Env: compactionTestOrigin, WorkingLoader: config.LoaderUI{Text: "working"},
		},
		Logging:    config.LoggingConfig{Level: "info", Format: "pretty"},
		Extensions: config.ExtensionsConfig{Use: []config.ExtensionUse{}, Enabled: false},
		Models: config.ModelsConfig{Discovery: config.ModelDiscoveryConfig{
			SourceURL: "", CacheTTL: 0, FetchTimeout: 0, Enabled: false,
		}},
		Assistant: config.AssistantConfig{
			Provider: compactionTestOrigin, Model: compactionTestOrigin, ThinkingLevel: "off",
			Retry: config.RetryConfig{BaseDelay: 0, MaxDelay: 0, MaxAttempts: 1, Enabled: false},
		},
		Database: config.DatabaseConfig{
			Path: "", ApplyMigrations: true, MaxOpenConns: 1, MaxIdleConns: 1,
			ConnMaxLifetime: 0, BusyTimeout: 0,
		},
		Context: config.ContextConfig{
			CompactionProvider:          "",
			CompactionModel:             "",
			ExtensionContributionTokens: 0,
			AutoCompactionThreshold:     0,
			RetainedTailMaxTokens:       0,
			SummaryOutputTokens:         0,
			AutoCompactionEnabled:       false,
			OutputReserveTokens:         0, ProviderReserveTokens: 0, SafetyMarginTokens: 0, PreflightEnabled: false,
		},
		Cache: config.CacheConfig{Enabled: false, Capacity: 0, TTL: 0},
		Tasks: config.TaskRuntimeConfig{
			Workers: 0, SessionWorkers: 0, PollInterval: 0, LeaseDuration: 0, Heartbeat: 0,
			RecoveryInterval: 0, DefaultTimeout: 0, MaxTimeout: 0, MaxOutcomeBytes: 0,
		},
	}
}

func repositoryNow() time.Time {
	return time.Now().UTC()
}
