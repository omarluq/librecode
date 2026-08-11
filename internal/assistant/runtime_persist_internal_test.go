package assistant

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/tool"
	"github.com/omarluq/librecode/internal/transcript"
)

const (
	runtimePersistTestArgsJSON = `{"path":"a.go"}`
	runtimePersistToolCallID   = "call-1"
	runtimePersistParentID     = "outer"
	runtimePersistChildOneID   = "outer/1"
)

func TestRuntime_PromptPersistenceContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		busyTimeout time.Duration
		writeCount  int
		wantMinimum time.Duration
	}{
		{name: "minimum window", busyTimeout: 0, writeCount: 1, wantMinimum: minimumPromptPersistenceTimeout},
		{
			name: "outlasts all SQLite busy retries", busyTimeout: 15 * time.Second,
			writeCount: 1, wantMinimum: 46 * time.Second,
		},
		{
			name: "saturates overflow", busyTimeout: time.Duration(math.MaxInt64),
			writeCount: 1, wantMinimum: time.Duration(math.MaxInt64),
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var runtimeConfig config.Config

			runtimeConfig.Database.BusyTimeout = testCase.busyTimeout
			runtime := newRuntimeFromDeps(func(deps *runtimeDeps) { deps.Config = &runtimeConfig })

			parent, cancelParent := context.WithCancel(t.Context())
			cancelParent()

			startedAt := time.Now()

			ctx, cancel := runtime.promptPersistenceContext(parent, testCase.writeCount)
			defer cancel()

			require.NoError(t, ctx.Err())
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			assert.False(t, deadline.Before(startedAt.Add(testCase.wantMinimum)))
		})
	}
}

func TestPromptPersistenceTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		busyTimeout time.Duration
		writeCount  int
		want        time.Duration
	}{
		{name: "disabled busy timeout uses minimum", busyTimeout: 0, writeCount: 1, want: 5 * time.Second},
		{name: "short busy timeout uses minimum", busyTimeout: time.Second, writeCount: 1, want: 5 * time.Second},
		{name: "covers all busy attempts", busyTimeout: 15 * time.Second, writeCount: 1, want: 46 * time.Second},
		{name: "covers every append", busyTimeout: 15 * time.Second, writeCount: 3, want: 138 * time.Second},
		{name: "minimum covers every append", busyTimeout: 0, writeCount: 3, want: 15 * time.Second},
		{
			name: "saturates busy timeout overflow", busyTimeout: time.Duration(math.MaxInt64),
			writeCount: 1, want: math.MaxInt64,
		},
		{
			name: "saturates operation count overflow", busyTimeout: 15 * time.Second,
			writeCount: math.MaxInt, want: math.MaxInt64,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, promptPersistenceTimeout(testCase.busyTimeout, testCase.writeCount))
		})
	}
}

func TestRespondWithPartialProgressPreservesPromptAndPersistenceErrors(t *testing.T) {
	t.Parallel()

	promptErr := errors.New("prompt failed")
	persistErr := errors.New("persistence failed")
	joined := failedPromptError(promptErr, persistErr)

	require.ErrorIs(t, joined, promptErr)
	require.ErrorIs(t, joined, persistErr)
	assert.Contains(t, joined.Error(), "prompt failed")
	assert.Contains(t, joined.Error(), "persist failed prompt progress")
}

func TestRuntime_AppendPartialPromptFailureKeepsEarlierMessagesWhenLaterAppendFails(t *testing.T) {
	t.Parallel()

	repository, connection := agentToolSessionsWithDB(t)
	ctx := t.Context()
	session, err := repository.CreateSession(ctx, t.TempDir(), "partial persistence", "")
	require.NoError(t, err)
	userEntry := appendRuntimeContextTestMessage(t, repository, session.ID, nil, database.RoleUser, "prompt")

	_, err = connection.ExecContext(ctx, `CREATE TRIGGER reject_partial_error_marker
BEFORE INSERT ON session_messages
WHEN NEW.role = 'custom'
BEGIN
    SELECT RAISE(FAIL, 'reject partial error marker');
END`)
	require.NoError(t, err)

	var runtimeConfig config.Config

	runtimeConfig.Database.BusyTimeout = time.Millisecond
	runtime := newRuntimeFromDeps(func(deps *runtimeDeps) {
		deps.Config = &runtimeConfig
		deps.Sessions = repository
	})
	progress := newPartialPromptProgress(nil)
	progress.append(transcript.RoleAssistant, "partial answer")

	promptErr := errors.New("prompt failed")
	persistErr := runtime.appendPartialPromptFailure(ctx, session.ID, userEntry.ID, progress, promptErr)

	require.Error(t, persistErr)

	messages, messagesErr := repository.Messages(ctx, session.ID)
	require.NoError(t, messagesErr)
	require.Len(t, messages, 2)
	assert.Equal(t, database.RoleAssistant, messages[1].Role)
	assert.Equal(t, "partial answer", messages[1].Content)

	joined := failedPromptError(promptErr, persistErr)
	require.ErrorIs(t, joined, promptErr)
	require.ErrorIs(t, joined, persistErr)
}

func TestFormatToolEventIncludesErrorMarker(t *testing.T) {
	t.Parallel()

	formatted := formatToolEvent(&ToolEvent{
		CallID:       "",
		ParentCallID: "",
		Sequence:     0,

		Name:          jsonBashToolName,
		ArgumentsJSON: `{"command":"false"}`,
		DetailsJSON:   `{"exit_code":1}`,
		Result:        "Command exited with code 1",
		Error:         "Command exited with code 1",
		IsError:       true,
	})

	assert.Contains(t, formatted, "tool: bash")
	assert.Contains(t, formatted, "error:\nCommand exited with code 1")
	assert.Contains(t, formatted, "is_error: true")
	assert.Contains(t, formatted, "details:\n{\"exit_code\":1}")
	assert.Contains(t, formatted, "output:\nCommand exited with code 1")
}

func TestPartialPromptProgressReplacesPendingToolWithResult(t *testing.T) {
	t.Parallel()

	progress := newPartialPromptProgress(nil)
	progress.record(StreamEvent{
		ToolCallEvent: runtimePersistTestToolCall(),
		ToolEvent:     nil,
		Usage:         nil,
		Kind:          StreamEventToolStart,
		Text:          jsonReadToolName,
	})
	progress.record(StreamEvent{
		ToolCallEvent: nil,
		ToolEvent:     runtimePersistTestToolResult(),
		Usage:         nil,
		Kind:          StreamEventToolResult,
		Text:          "",
	})

	assert.Empty(t, progress.syntheticToolFailureEvents(context.Canceled))
}

func TestPartialPromptProgressFallsBackWhenResultHasGeneratedCallID(t *testing.T) {
	t.Parallel()

	progress := newPartialPromptProgress(nil)
	call := runtimePersistTestToolCall()
	call.ID = ""
	progress.trackPendingTool(call, "")

	result := runtimePersistTestToolResult()
	result.CallID = "generated-call-id"
	progress.removePendingTool(result)

	assert.Empty(t, progress.pendingTools)
}

func TestPartialPromptProgressCollectsNestedToolsAndMatchesByCallID(t *testing.T) {
	t.Parallel()

	progress := newPartialPromptProgress(nil)
	first := runtimePersistTestToolCall()
	first.ParentCallID = runtimePersistParentID
	first.Sequence = 1
	second := runtimePersistTestToolCall()
	second.ID = "call-2"
	second.ParentCallID = runtimePersistParentID
	second.Sequence = 2

	progress.trackPendingTool(first, "")
	progress.trackPendingTool(second, "")

	result := runtimePersistTestToolResult()
	result.CallID = second.ID
	result.ParentCallID = second.ParentCallID
	result.Sequence = second.Sequence
	progress.record(StreamEvent{
		ToolCallEvent: nil, ToolEvent: result, Usage: nil, Kind: StreamEventToolResult, Text: "",
	})

	require.Len(t, progress.pendingTools, 1)
	assert.Equal(t, first.ID, progress.pendingTools[0].CallID)
	require.Len(t, progress.completedNestedTools, 1)
	assert.Equal(t, second.ID, progress.completedNestedTools[0].CallID)
}

func TestMergeNestedToolEventsPreservesParentExecutionOrder(t *testing.T) {
	t.Parallel()

	outer := []ToolEvent{
		runtimePersistIdentityEvent("outer-1", "", "execute", 0),
		runtimePersistIdentityEvent("outer-2", "", "execute", 0),
	}
	nested := []ToolEvent{
		runtimePersistIdentityEvent("outer-1/1", "outer-1", "read", 1),
		runtimePersistIdentityEvent("outer-2/1", "outer-2", "grep", 1),
		runtimePersistIdentityEvent("outer-1/2", "outer-1", "find", 2),
	}

	merged := mergeNestedToolEvents(outer, nested)

	require.Len(t, merged, 5)
	assert.Equal(t, []string{"outer-1/1", "outer-1/2", "outer-1", "outer-2/1", "outer-2"}, []string{
		merged[0].CallID, merged[1].CallID, merged[2].CallID, merged[3].CallID, merged[4].CallID,
	})
}

func TestMergeNestedToolEventsOrdersSiblingsBySequence(t *testing.T) {
	t.Parallel()

	outer := []ToolEvent{runtimePersistIdentityEvent(runtimePersistParentID, "", "execute", 0)}
	nested := []ToolEvent{
		runtimePersistIdentityEvent("outer/3", runtimePersistParentID, "write", 3),
		runtimePersistIdentityEvent(runtimePersistChildOneID, runtimePersistParentID, "read", 1),
		runtimePersistIdentityEvent("outer/unknown", runtimePersistParentID, "grep", 0),
		runtimePersistIdentityEvent("outer/2", runtimePersistParentID, "find", 2),
	}

	merged := mergeNestedToolEvents(outer, nested)

	wantCallIDs := []string{
		runtimePersistChildOneID, "outer/2", "outer/3", "outer/unknown", runtimePersistParentID,
	}
	assert.Equal(t, wantCallIDs, toolEventCallIDs(merged))
}

func TestMergeNestedToolEventsPreservesNestedHierarchy(t *testing.T) {
	t.Parallel()

	outer := []ToolEvent{runtimePersistIdentityEvent(runtimePersistParentID, "", "execute", 0)}
	nested := []ToolEvent{
		runtimePersistIdentityEvent("child", runtimePersistParentID, "execute", 1),
		runtimePersistIdentityEvent("grandchild", "child", "read", 1),
	}

	merged := mergeNestedToolEvents(outer, nested)

	assert.Equal(t, []string{"grandchild", "child", runtimePersistParentID}, toolEventCallIDs(merged))
}

func TestMergeNestedToolEventsKeepsOrphansInStreamOrder(t *testing.T) {
	t.Parallel()

	nested := []ToolEvent{
		runtimePersistIdentityEvent("orphan-2", "missing", "grep", 2),
		runtimePersistIdentityEvent("root", "", "read", 0),
		runtimePersistIdentityEvent("orphan-1", "missing", "find", 1),
	}

	merged := mergeNestedToolEvents(nil, nested)

	assert.Equal(t, []string{"orphan-2", "root", "orphan-1"}, toolEventCallIDs(merged))
}

func TestPartialPromptProgressResetClearsPendingTools(t *testing.T) {
	t.Parallel()

	progress := newPartialPromptProgress(nil)
	progress.record(StreamEvent{
		ToolCallEvent: runtimePersistTestToolCall(),
		ToolEvent:     nil,
		Usage:         nil,
		Kind:          StreamEventToolStart,
		Text:          jsonReadToolName,
	})

	progress.reset()

	assert.Empty(t, progress.syntheticToolFailureEvents(context.Canceled))
}

func toolEventCallIDs(events []ToolEvent) []string {
	callIDs := make([]string, 0, len(events))
	for index := range events {
		callIDs = append(callIDs, events[index].CallID)
	}

	return callIDs
}

func runtimePersistIdentityEvent(callID, parentCallID, name string, sequence int) ToolEvent {
	return ToolEvent{
		CallID: callID, ParentCallID: parentCallID, Sequence: sequence,
		Name: name, ArgumentsJSON: "", DetailsJSON: "", Result: "", Error: "", IsError: false,
	}
}

func runtimePersistTestToolCall() *ToolCallEvent {
	return &ToolCallEvent{
		ParentCallID: "",
		Sequence:     0,

		ArgumentsJSON: runtimePersistTestArgsJSON,
		ID:            runtimePersistToolCallID,
		Name:          jsonReadToolName,
		Arguments:     tool.EmptyArguments(),
	}
}

func runtimePersistTestToolResult() *ToolEvent {
	return &ToolEvent{
		CallID:       "",
		ParentCallID: "",
		Sequence:     0,

		Name:          jsonReadToolName,
		ArgumentsJSON: runtimePersistTestArgsJSON,
		DetailsJSON:   "",
		Result:        "ok",
		Error:         "",
		IsError:       false,
	}
}
