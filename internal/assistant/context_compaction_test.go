package assistant_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/auth"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/event"
	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/model"
)

const compactedWorkSummary = "summary of compacted work"

func TestRuntime_CompactSessionSummarizesOldHistoryAndKeepsTail(t *testing.T) {
	t.Parallel()

	client := &compactionCompleter{summary: compactedWorkSummary, requests: nil}
	runtime, repository := newCompactionRuntimeForTailPolicy(t, client, 1_000)
	ctx := context.Background()
	session, err := repository.CreateSession(ctx, testRuntimeCWD, "compact", "")
	require.NoError(t, err)
	first := appendRuntimeTestMessage(t, repository, session.ID, nil, database.RoleUser, "old user goal")
	second := appendRuntimeTestMessage(
		t,
		repository,
		session.ID,
		&first.ID,
		database.RoleAssistant,
		"old assistant answer",
	)
	third := appendRuntimeTestMessage(t, repository, session.ID, &second.ID, database.RoleUser, "recent user tail")
	fourth := appendRuntimeTestMessage(
		t,
		repository,
		session.ID,
		&third.ID,
		database.RoleAssistant,
		"recent assistant tail",
	)

	entry, err := runtime.CompactSession(ctx, session.ID, testRuntimeCWD)

	require.NoError(t, err)
	require.NotNil(t, entry)
	assert.Equal(t, database.EntryTypeCompaction, entry.Type)
	assert.Equal(t, compactedWorkSummary, entry.Summary)
	assert.Equal(t, third.ID, entry.CompactionFirstKeptEntryID)
	assert.Positive(t, entry.CompactionTokensBefore)
	require.NotNil(t, entry.ParentID)
	assert.Equal(t, fourth.ID, *entry.ParentID)
	require.Len(t, client.requests, 1)
	assert.True(t, client.requests[0].DisableTools)
	assert.Contains(t, client.requests[0].SystemPrompt, "Summarize the conversation history")
	require.Len(t, client.requests[0].Messages, 2)
	assert.Equal(t, "old user goal", client.requests[0].Messages[0].Content)
	assert.Equal(t, "old assistant answer", client.requests[0].Messages[1].Content)

	contextEntity, err := repository.BuildContext(ctx, session.ID, entry.ID)
	require.NoError(t, err)
	require.Len(t, contextEntity.Messages, 3)
	assert.Equal(t, database.RoleCompactionSummary, contextEntity.Messages[0].Role)
	assert.Equal(t, compactedWorkSummary, contextEntity.Messages[0].Content)
	assert.Equal(t, "recent user tail", contextEntity.Messages[1].Content)
	assert.Equal(t, "recent assistant tail", contextEntity.Messages[2].Content)
}

func TestRuntime_CompactSessionUsesDynamicModelContextTail(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name                    string
		messageTokens           []int
		contextWindow           int
		wantFirstKeptEntryIndex int
		wantSummaryMessageCount int
	}{
		{
			name:                    "dynamic keeps newest third capped by current branch",
			contextWindow:           100_000,
			messageTokens:           []int{10_000, 10_000, 15_000, 15_000},
			wantFirstKeptEntryIndex: 2,
			wantSummaryMessageCount: 2,
		},
		{
			name:                    "unknown context window uses fallback capped by current branch",
			contextWindow:           0,
			messageTokens:           []int{10_000, 10_000, 15_000, 15_000},
			wantFirstKeptEntryIndex: 2,
			wantSummaryMessageCount: 2,
		},
		{
			name:                    "tiny context window still keeps a positive recent tail",
			contextWindow:           2,
			messageTokens:           []int{10_000, 10_000, 15_000, 15_000},
			wantFirstKeptEntryIndex: 3,
			wantSummaryMessageCount: 2,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			client := &compactionCompleter{summary: compactedWorkSummary, requests: nil}
			runtime, repository := newCompactionRuntimeForTailPolicy(
				t,
				client,
				testCase.contextWindow,
			)
			ctx := context.Background()
			session, err := repository.CreateSession(ctx, testRuntimeCWD, "compact", "")
			require.NoError(t, err)
			entries := appendRuntimeTestTokenMessages(t, repository, session.ID, testCase.messageTokens)

			entry, err := runtime.CompactSession(ctx, session.ID, testRuntimeCWD)

			require.NoError(t, err)
			require.NotNil(t, entry)
			assert.Equal(t, entries[testCase.wantFirstKeptEntryIndex].ID, entry.CompactionFirstKeptEntryID)
			require.NotNil(t, entry.ParentID)
			assert.Equal(t, entries[len(entries)-1].ID, *entry.ParentID)
			require.Len(t, client.requests, 1)
			require.Len(t, client.requests[0].Messages, testCase.wantSummaryMessageCount)

			for index := range client.requests[0].Messages {
				assert.Equal(t, entries[index].Message.Content, client.requests[0].Messages[index].Content)
			}
		})
	}
}

func TestRuntime_CompactSessionChainsNextPromptFromCompactionEntry(t *testing.T) {
	t.Parallel()

	client := &compactionCompleter{summary: compactedWorkSummary, requests: nil}
	runtime, repository := newCompactionRuntimeForTailPolicy(t, client, 2)
	ctx := context.Background()
	session, err := repository.CreateSession(ctx, testRuntimeCWD, "compact", "")
	require.NoError(t, err)
	old := appendRuntimeTestMessage(t, repository, session.ID, nil, database.RoleUser, strings.Repeat("old ", 1000))
	appendRuntimeTestMessage(t, repository, session.ID, &old.ID, database.RoleAssistant, "tail")

	compactionEntry, err := runtime.CompactSession(ctx, session.ID, testRuntimeCWD)
	require.NoError(t, err)
	followUp := appendRuntimeTestMessage(t, repository, session.ID, &compactionEntry.ID, database.RoleUser, "continue")

	contextEntity, err := repository.BuildContext(ctx, session.ID, followUp.ID)

	require.NoError(t, err)
	require.Len(t, contextEntity.Messages, 3)
	assert.Equal(t, database.RoleCompactionSummary, contextEntity.Messages[0].Role)
	assert.Equal(t, "tail", contextEntity.Messages[1].Content)
	assert.Equal(t, "continue", contextEntity.Messages[2].Content)
}

func TestRuntime_CompactSessionRejectsShortHistory(t *testing.T) {
	t.Parallel()

	client := &compactionCompleter{summary: autoCompactionTestUnused, requests: nil}
	runtime, repository := newTestRuntimeWithClient(t, client)
	ctx := context.Background()
	session, err := repository.CreateSession(ctx, testRuntimeCWD, "compact", "")
	require.NoError(t, err)
	appendRuntimeTestMessage(t, repository, session.ID, nil, database.RoleUser, "only one message")

	entry, err := runtime.CompactSession(ctx, session.ID, testRuntimeCWD)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not enough old history to compact")
	assert.Nil(t, entry)
	assert.Empty(t, client.requests)
}

func TestRuntime_CompactSessionFromUsesExplicitParent(t *testing.T) {
	t.Parallel()

	client := &compactionCompleter{summary: "summary of selected branch", requests: nil}
	runtime, repository := newCompactionRuntimeForTailPolicy(t, client, 2)
	ctx := context.Background()
	session, err := repository.CreateSession(ctx, testRuntimeCWD, "compact", "")
	require.NoError(t, err)
	root := appendRuntimeTestMessage(t, repository, session.ID, nil, database.RoleUser, strings.Repeat("root ", 1000))
	selectedTail := appendRuntimeTestMessage(
		t,
		repository,
		session.ID,
		&root.ID,
		database.RoleAssistant,
		"selected tail",
	)
	_ = appendRuntimeTestMessage(
		t,
		repository,
		session.ID,
		&root.ID,
		database.RoleAssistant,
		"other branch tail",
	)

	entry, err := runtime.CompactSessionFrom(ctx, session.ID, testRuntimeCWD, &selectedTail.ID)

	require.NoError(t, err)
	require.NotNil(t, entry.ParentID)
	assert.Equal(t, selectedTail.ID, *entry.ParentID)
	assert.Equal(t, selectedTail.ID, entry.CompactionFirstKeptEntryID)
	require.Len(t, client.requests, 1)
	require.Len(t, client.requests[0].Messages, 1)
	assert.Equal(t, root.Message.Content, client.requests[0].Messages[0].Content)
}

func TestRuntime_CompactSessionReplansHookSelectedFirstKeptEntry(t *testing.T) {
	t.Parallel()

	client := &compactionCompleter{summary: compactedWorkSummary, requests: nil}
	_, repository, manager := newTestRuntimeWithManager(t, client)
	runtime := newCompactionRuntimeWithManagerWindow(t, repository, manager, client)
	loadRuntimeExtension(t, manager, `
local lc = require("librecode")
lc.on("session_before_compact", function(event)
  return {
    compaction = {
      first_kept_entry_id = event.payload.kept_entry_ids[2],
    }
  }
end)
`)

	ctx := context.Background()
	session, err := repository.CreateSession(ctx, testRuntimeCWD, "compact", "")
	require.NoError(t, err)
	old := appendRuntimeTestMessage(t, repository, session.ID, nil, database.RoleUser, strings.Repeat("old ", 1000))
	firstTail := appendRuntimeTestMessage(t, repository, session.ID, &old.ID, database.RoleAssistant, "first tail")
	selectedTail := appendRuntimeTestMessage(
		t,
		repository,
		session.ID,
		&firstTail.ID,
		database.RoleAssistant,
		"selected tail",
	)

	entry, err := runtime.CompactSession(ctx, session.ID, testRuntimeCWD)

	require.NoError(t, err)
	assert.Equal(t, selectedTail.ID, entry.CompactionFirstKeptEntryID)
	require.Len(t, client.requests, 1)
	require.Len(t, client.requests[0].Messages, 2)
	assert.Equal(t, old.Message.Content, client.requests[0].Messages[0].Content)
	assert.Equal(t, firstTail.Message.Content, client.requests[0].Messages[1].Content)
}

func TestRuntime_CompactSessionReplansHookFileOperations(t *testing.T) {
	t.Parallel()

	client := &compactionCompleter{summary: compactedWorkSummary, requests: nil}
	_, repository, manager := newTestRuntimeWithManager(t, client)
	runtime := newCompactionRuntimeWithManagerWindow(t, repository, manager, client)
	ctx := context.Background()
	session, err := repository.CreateSession(ctx, testRuntimeCWD, "compact", "")
	require.NoError(t, err)
	old := appendRuntimeTestMessage(t, repository, session.ID, nil, database.RoleUser, strings.Repeat("old ", 1000))
	firstRead := appendRuntimeTestToolResult(
		t,
		repository,
		session.ID,
		&old.ID,
		"read",
		`{"path":"first.txt"}`,
		"first file",
	)
	selectedTail := appendRuntimeTestMessage(
		t,
		repository,
		session.ID,
		&firstRead.ID,
		database.RoleAssistant,
		"selected tail",
	)
	trailingRead := appendRuntimeTestToolResult(
		t,
		repository,
		session.ID,
		&selectedTail.ID,
		"read",
		`{"path":"tail.txt"}`,
		"tail file",
	)
	tail := appendRuntimeTestMessage(t, repository, session.ID, &trailingRead.ID, database.RoleAssistant, "tail")
	loadRuntimeExtension(t, manager, fmt.Sprintf(`
local lc = require("librecode")
lc.on("session_before_compact", function()
  return {
    compaction = {
      first_kept_entry_id = %q,
      summary = "hook summary",
    }
  }
end)
`, tail.ID))

	entry, err := runtime.CompactSession(ctx, session.ID, testRuntimeCWD)

	require.NoError(t, err)
	assert.Equal(t, tail.ID, entry.CompactionFirstKeptEntryID)
	assert.Contains(t, entry.Summary, "read: first.txt")
	assert.Contains(t, entry.Summary, "read: tail.txt")
}

func TestRuntime_CompactSessionRejectsEmptySummary(t *testing.T) {
	t.Parallel()

	client := &compactionCompleter{summary: "   ", requests: nil}
	runtime, repository := newCompactionRuntimeForTailPolicy(t, client, 2)
	ctx := context.Background()
	session, err := repository.CreateSession(ctx, testRuntimeCWD, "compact", "")
	require.NoError(t, err)
	old := appendRuntimeTestMessage(t, repository, session.ID, nil, database.RoleUser, strings.Repeat("old ", 1000))
	appendRuntimeTestMessage(t, repository, session.ID, &old.ID, database.RoleAssistant, "tail")

	entry, err := runtime.CompactSession(ctx, session.ID, testRuntimeCWD)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "compaction summary was empty")
	assert.Nil(t, entry)
}

func TestRuntime_CompactSessionPreservesFileOperations(t *testing.T) {
	t.Parallel()

	client := &compactionCompleter{summary: compactedWorkSummary, requests: nil}
	runtime, repository := newCompactionRuntimeForTailPolicy(t, client, 2)
	ctx := context.Background()
	session, err := repository.CreateSession(ctx, testRuntimeCWD, "compact", "")
	require.NoError(t, err)
	user := appendRuntimeTestMessage(
		t,
		repository,
		session.ID,
		nil,
		database.RoleUser,
		strings.Repeat("inspect files ", 200),
	)
	readEntry := appendRuntimeTestToolResult(
		t,
		repository,
		session.ID,
		&user.ID,
		"read",
		`{"path":"internal/assistant/runtime.go"}`,
		"runtime file",
	)
	writeEntry := appendRuntimeTestToolResult(
		t,
		repository,
		session.ID,
		&readEntry.ID,
		"write",
		`{"path":"internal/assistant/new_file.go"}`,
		"wrote file",
	)
	bashEntry := appendRuntimeTestToolResult(
		t,
		repository,
		session.ID,
		&writeEntry.ID,
		"bash",
		`{"command":"sed -i 's/a/b/' internal/assistant/runtime.go"}`,
		"edited",
	)
	appendRuntimeTestMessage(t, repository, session.ID, &bashEntry.ID, database.RoleAssistant, "tail")

	entry, err := runtime.CompactSession(ctx, session.ID, testRuntimeCWD)

	require.NoError(t, err)
	assert.Contains(t, entry.Summary, "File operations preserved during compaction:")
	assert.Contains(t, entry.Summary, "read: internal/assistant/runtime.go")
	assert.Contains(t, entry.Summary, "modified: internal/assistant/new_file.go")
	assert.Contains(t, entry.Summary, "modified: internal/assistant/runtime.go")

	data := map[string]any{}
	require.NoError(t, json.Unmarshal([]byte(entry.DataJSON), &data))
	details, ok := data["details"].(map[string]any)
	require.True(t, ok)
	operations, ok := details["file_operations"].([]any)
	require.True(t, ok)
	assert.Len(t, operations, 3)
}

func repeatedTokenEstimate(tokens int) string {
	return strings.Repeat("x", tokens*4)
}

func newCompactionRuntimeForTailPolicy(
	t *testing.T,
	client assistant.Completer,
	contextWindow int,
) (*assistant.Runtime, *database.SessionRepository) {
	t.Helper()

	_, repository := newTestRuntimeWithClient(t, client)
	runtimeConfig := testConfig()

	return assistant.NewRuntime(&assistant.RuntimeOptions{
		Config:      runtimeConfig,
		Sessions:    repository,
		Extensions:  nil,
		Cache:       assistant.NewResponseCache(false, 1, time.Minute),
		Events:      event.NewBus(nil),
		Models:      newCompactionTestRegistry(t, contextWindow),
		Client:      client,
		Logger:      nil,
		SkillsCache: nil,
	}), repository
}

func appendRuntimeTestTokenMessages(
	t *testing.T,
	repository *database.SessionRepository,
	sessionID string,
	messageTokens []int,
) []*database.EntryEntity {
	t.Helper()

	entries := make([]*database.EntryEntity, 0, len(messageTokens))
	roles := []database.Role{database.RoleUser, database.RoleAssistant}

	var parentID *string
	for index, tokens := range messageTokens {
		entry := appendRuntimeTestMessage(
			t,
			repository,
			sessionID,
			parentID,
			roles[index%len(roles)],
			repeatedTokenEstimate(tokens),
		)
		entries = append(entries, entry)
		parentID = &entry.ID
	}

	return entries
}

func newCompactionTestRegistry(t *testing.T, contextWindow int) *model.Registry {
	t.Helper()

	storage, err := auth.NewInMemoryStorage(context.Background(), map[string]auth.Credential{
		testRuntimeProvider: testProviderCredential(),
	})
	require.NoError(t, err)

	return model.NewRegistry(&model.RegistryOptions{
		ConfigReader: nil,
		Auth:         storage,
		ModelsPath:   "",
		BuiltIns:     []model.Model{testRuntimeModelWithContextWindowAndMaxTokens(contextWindow, 0)},
		Discovery:    disabledModelDiscovery(),
	})
}

func appendRuntimeTestMessage(
	t *testing.T,
	repository *database.SessionRepository,
	sessionID string,
	parentID *string,
	role database.Role,
	content string,
) *database.EntryEntity {
	t.Helper()

	entry, err := repository.AppendMessage(context.Background(), sessionID, parentID, &database.MessageEntity{
		Timestamp: time.Now().UTC(),
		Role:      role,
		Content:   content,
		Provider:  "",
		Model:     "",
	})
	require.NoError(t, err)

	return entry
}

func appendRuntimeTestToolResult(
	t *testing.T,
	repository *database.SessionRepository,
	sessionID string,
	parentID *string,
	name string,
	argsJSON string,
	result string,
) *database.EntryEntity {
	t.Helper()

	return appendRuntimeTestMessage(
		t,
		repository,
		sessionID,
		parentID,
		database.RoleToolResult,
		strings.Join([]string{"tool: " + name, "arguments:", argsJSON, "output:", result}, "\n"),
	)
}

type compactionCompleter struct {
	summary  string
	requests []*assistant.CompletionRequest
}

func (client *compactionCompleter) Complete(
	_ context.Context,
	request *assistant.CompletionRequest,
) (*assistant.CompletionResult, error) {
	client.requests = append(client.requests, request)

	text := client.summary
	if text == "" {
		text = "summary: " + strings.TrimSpace(request.Messages[0].Content)
	}

	return &assistant.CompletionResult{
		FinishReason: llm.FinishReasonStop,
		Text:         text,
		Thinking:     nil,
		ToolEvents:   nil,
		Usage:        model.EmptyTokenUsage(),
	}, nil
}
