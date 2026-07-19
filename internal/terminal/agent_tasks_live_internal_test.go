package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/tool"
	"github.com/omarluq/librecode/internal/transcript"
)

const (
	inspectedToolCallID = "call-1"
	gapTextOne          = "gap one"
	gapTextTwo          = "gap two"
	gapTextThree        = "gap three"
)

func TestInspectedAgentTaskRendersLiveStreamAndReloadsOnCompletion(t *testing.T) {
	t.Parallel()

	connection := newPromptSendTestConnection(t)
	sessions := database.NewSessionRepository(connection)
	parent, err := sessions.CreateSession(t.Context(), t.TempDir(), "parent", "")
	require.NoError(t, err)
	child, err := sessions.CreateSession(t.Context(), parent.CWD, "child", parent.ID)
	require.NoError(t, err)

	task := behaviorAgentTask(behaviorTaskID, database.TaskRunning)
	task.Task.OwnerSessionID = parent.ID
	task.ChildSessionID = child.ID
	stub := newAgentTaskControllerStub(map[string]*database.AgentTaskEntity{task.Task.ID: &task}, nil)
	runtime := assistant.NewRuntimeForTest(func(options *assistant.RuntimeTestOptions) {
		options.Sessions = sessions
	})
	runtime.SetAgentTaskController(stub)

	app := newRenderTestApp(t)
	app.runtime = runtime
	app.sessionID = child.ID
	app.agentTaskSessionStack = []string{parent.ID}

	app.applyInspectedAgentTaskEvent(t.Context(), task.Task.ID, taskStreamPayload(t, assistant.StreamEvent{
		ToolCallEvent: nil, ToolEvent: nil, Usage: nil,
		Kind: assistant.StreamEventThinkingDelta, Text: "considering the code",
	}))
	app.applyInspectedAgentTaskEvent(t.Context(), task.Task.ID, taskStreamPayload(t, assistant.StreamEvent{
		ToolCallEvent: nil, ToolEvent: nil, Usage: nil,
		Kind: assistant.StreamEventTextDelta, Text: "working on it",
	}))

	require.Len(t, app.transcript.Streaming.Blocks, 2)
	assert.Equal(t, transcript.RoleThinking, app.transcript.Streaming.Blocks[0].Role)
	assert.Equal(t, "working on it", app.transcript.Streaming.Blocks[1].Content)

	_, err = sessions.AppendMessage(t.Context(), child.ID, nil, &database.MessageEntity{
		Timestamp: time.Now().UTC(), Role: database.RoleAssistant, Content: "finished result",
		Provider: "provider", Model: "model",
	})
	require.NoError(t, err)

	task.Task.State = database.TaskSucceeded

	app.handleAgentTaskTerminalEvent(t.Context(), task.Task.ID)

	assert.Empty(t, app.transcript.Streaming.Blocks)
	require.Len(t, app.transcript.History, 2)
	assert.Equal(t, "finished result", app.transcript.History[0].Content)
	assert.Contains(t, app.transcript.History[1].Content, "inspecting agent task")
}

func TestInspectedAgentTaskRendersToolResultAndCompactionNotices(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	toolResult := agentToolEvent(testToolRead, "", false)
	toolResult.CallID = inspectedToolCallID
	toolResult.Result = "file contents"
	app.renderInspectedAgentTaskEvent(asyncTestEventWithTool(toolResult))

	require.Len(t, app.transcript.Streaming.Blocks, 1)
	assert.Equal(t, transcript.RoleToolResult, app.transcript.Streaming.Blocks[0].Role)
	assert.Contains(t, app.transcript.Streaming.Blocks[0].Content, "file contents")

	for _, notice := range []struct {
		kind asyncEventKind
		text string
	}{
		{asyncEventPromptContext, "compaction planned"},
		{asyncEventCompactStart, "compaction started"},
		{asyncEventCompactDone, "compaction completed"},
		{asyncEventCompactError, "compaction failed"},
	} {
		app.renderInspectedAgentTaskEvent(asyncTestEvent(notice.kind, "", notice.text, 0))
	}

	notices := []string{
		"compaction planned", "compaction started", "compaction completed", "compaction failed",
	}
	require.Len(t, app.transcript.History, len(notices))

	for index, notice := range notices {
		assert.Contains(t, app.transcript.History[index].Content, notice)
	}
}

func TestInspectedAgentTaskStreamDoesNotEmitExtensionEvents(t *testing.T) {
	t.Parallel()

	app := newExtensionRuntimeTestApp(t, `
librecode.on("model_delta", function() librecode.buf.append("events", "model\n") end)
librecode.on("thinking_delta", function() librecode.buf.append("events", "thinking\n") end)
librecode.on("tool_start", function() librecode.buf.append("events", "tool_start\n") end)
librecode.on("tool_end", function() librecode.buf.append("events", "tool_end\n") end)
`)
	call := &assistant.ToolCallEvent{
		Arguments: tool.EmptyArguments(), ID: inspectedToolCallID, ParentCallID: "", Name: testToolRead,
		ArgumentsJSON: `{"path":"main.go"}`, Sequence: 0,
	}
	result := agentToolEvent(testToolRead, `{"path":"main.go"}`, false)
	result.CallID = call.ID
	result.Result = "contents"

	app.renderInspectedAgentTaskEvent(asyncTestEvent(asyncEventPromptDelta, "", "text", 0))
	app.renderInspectedAgentTaskEvent(asyncTestEvent(asyncEventPromptThinkingDelta, "", "thought", 0))
	start := asyncTestEvent(asyncEventPromptToolStart, "", "", 0)
	start.ToolCallEvent = call
	app.renderInspectedAgentTaskEvent(start)
	app.renderInspectedAgentTaskEvent(asyncTestEventWithTool(result))

	assert.NotEmpty(t, app.transcript.Streaming.Blocks)
	_, emitted := app.extensionUI.Buffers["events"]
	assert.False(t, emitted, "inspection rendering must not emit runtime extension events")
}

func TestInspectedAgentTaskToolResultsHaveNoOperationalSideEffects(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.agentTasks = []database.AgentTaskEntity{behaviorAgentTask("existing", database.TaskRunning)}

	workflowResult := agentToolEvent(workflowToolName, `{"run_id":"historical-run"}`, false)
	workflowResult.CallID = "workflow-call"
	workflowResult.Result = "submitted"
	app.renderInspectedAgentTaskEvent(asyncTestEventWithTool(workflowResult))

	agentResult := agentToolEvent(agentStartToolName, `{"task_id":"historical-task"}`, false)
	agentResult.CallID = "agent-call"
	agentResult.Result = "submitted"
	app.renderInspectedAgentTaskEvent(asyncTestEventWithTool(agentResult))

	assert.Empty(t, app.activeWorkflows)
	require.Len(t, app.agentTasks, 1)
	assert.Equal(t, "existing", app.agentTasks[0].Task.ID)
}

func TestInspectRunningAgentTaskReplaysActivityEmittedBeforeSubscription(t *testing.T) {
	t.Parallel()

	connection := newPromptSendTestConnection(t)
	sessions := database.NewSessionRepository(connection)
	parent, err := sessions.CreateSession(t.Context(), t.TempDir(), "parent", "")
	require.NoError(t, err)
	child, err := sessions.CreateSession(t.Context(), parent.CWD, "child", parent.ID)
	require.NoError(t, err)

	task := behaviorAgentTask(behaviorTaskID, database.TaskRunning)
	task.Task.OwnerSessionID = parent.ID
	task.ChildSessionID = child.ID
	controller := &replayAgentTaskController{
		agentTaskControllerStub: newAgentTaskControllerStub(
			map[string]*database.AgentTaskEntity{task.Task.ID: &task},
			[]database.AgentTaskEntity{task},
		),
		events: []database.TaskEventEntity{
			taskStreamEvent(task.Task.ID, 1, assistant.StreamEvent{
				ToolCallEvent: nil, ToolEvent: nil, Usage: nil,
				Kind: assistant.StreamEventTextDelta, Text: "already working",
			}),
			taskStreamEvent(task.Task.ID, 2, assistant.StreamEvent{
				ToolCallEvent: &assistant.ToolCallEvent{
					Arguments: tool.EmptyArguments(), ID: inspectedToolCallID, ParentCallID: "",
					Name: testToolRead, ArgumentsJSON: `{"path":"main.go"}`, Sequence: 0,
				},
				ToolEvent: nil, Usage: nil, Kind: assistant.StreamEventToolStart, Text: "",
			}),
		},
	}
	runtime := assistant.NewRuntimeForTest(func(options *assistant.RuntimeTestOptions) {
		options.Sessions = sessions
	})
	runtime.SetAgentTaskController(controller)

	screen := newClipboardScreen()
	app := newRenderTestApp(t)
	app.screen = screen
	app.runtime = runtime
	app.sessionID = parent.ID
	app.agentTasks = []database.AgentTaskEntity{task}
	app.agentTaskSummaryOwnerID = parent.ID

	require.NoError(t, app.inspectAgentTask(t.Context(), task.Task.ID))
	defer app.stopAgentTaskWatches()

	for range controller.events {
		select {
		case event := <-screen.EventQ():
			interrupt, ok := event.(*tcell.EventInterrupt)
			require.True(t, ok)
			_, err = app.handleInterrupt(t.Context(), interrupt)
			require.NoError(t, err)
		case <-time.After(time.Second):
			require.FailNow(t, "timed out waiting for replayed task activity")
		}
	}

	require.Len(t, app.transcript.Streaming.Blocks, 1)
	assert.Equal(t, "already working", app.transcript.Streaming.Blocks[0].Content)
	require.Len(t, app.runningToolBlocks, 1)
	assert.Equal(t, testToolRead, app.runningToolBlocks[0].Call.Name)
}

func TestNestedAgentTaskReturnResumesAndReplaysParentActivity(t *testing.T) {
	t.Parallel()

	connection := newPromptSendTestConnection(t)
	sessions := database.NewSessionRepository(connection)
	root, err := sessions.CreateSession(t.Context(), t.TempDir(), "root", "")
	require.NoError(t, err)
	parent, err := sessions.CreateSession(t.Context(), root.CWD, "parent agent", root.ID)
	require.NoError(t, err)
	nested, err := sessions.CreateSession(t.Context(), root.CWD, "nested agent", parent.ID)
	require.NoError(t, err)

	parentTask := behaviorAgentTask("parent-task", database.TaskRunning)
	parentTask.Task.OwnerSessionID = root.ID
	parentTask.ChildSessionID = parent.ID
	controller := &replayAgentTaskController{
		agentTaskControllerStub: newAgentTaskControllerStub(
			map[string]*database.AgentTaskEntity{parentTask.Task.ID: &parentTask},
			[]database.AgentTaskEntity{parentTask},
		),
		events: []database.TaskEventEntity{taskStreamEvent(parentTask.Task.ID, 1, assistant.StreamEvent{
			ToolCallEvent: nil, ToolEvent: nil, Usage: nil,
			Kind: assistant.StreamEventTextDelta, Text: "parent resumed activity",
		})},
	}
	runtime := assistant.NewRuntimeForTest(func(options *assistant.RuntimeTestOptions) { options.Sessions = sessions })
	runtime.SetAgentTaskController(controller)

	screen := newClipboardScreen()
	app := newRenderTestApp(t)
	app.screen = screen
	app.runtime = runtime
	app.sessionID = nested.ID
	app.agentTaskSessionStack = []string{root.ID, parent.ID}
	app.transcript.Streaming.Blocks = []chatMessage{newChatMessage(transcript.RoleAssistant, "nested stale stream")}

	require.NoError(t, app.leaveAgentTaskSession(t.Context()))
	defer app.stopAgentTaskWatches()

	assert.Equal(t, parent.ID, app.sessionID)
	assert.Equal(t, []string{root.ID}, app.agentTaskSessionStack)

	select {
	case event := <-screen.EventQ():
		interrupt, ok := event.(*tcell.EventInterrupt)
		require.True(t, ok)
		_, err = app.handleInterrupt(t.Context(), interrupt)
		require.NoError(t, err)
	case <-time.After(time.Second):
		require.FailNow(t, "timed out waiting for resumed parent replay")
	}

	require.Len(t, app.transcript.Streaming.Blocks, 1)
	assert.Equal(t, "parent resumed activity", app.transcript.Streaming.Blocks[0].Content)
}

func TestAgentTaskWatcherReplaysLiveSequenceGap(t *testing.T) {
	t.Parallel()

	const taskID = "watcher-gap-task"

	controller := &stagedReplayAgentTaskController{
		agentTaskControllerStub: newAgentTaskControllerStub(nil, nil),
		batches: [][]database.TaskEventEntity{
			{taskStreamEvent(taskID, 1, assistant.StreamEvent{
				ToolCallEvent: nil, ToolEvent: nil, Usage: nil,
				Kind: assistant.StreamEventTextDelta, Text: gapTextOne,
			})},
			{taskStreamEvent(taskID, 2, assistant.StreamEvent{
				ToolCallEvent: nil, ToolEvent: nil, Usage: nil,
				Kind: assistant.StreamEventTextDelta, Text: gapTextTwo,
			})},
		},
		calls: 0,
		mu:    sync.Mutex{},
	}
	runtime := assistant.NewRuntimeForTest(nil)
	runtime.SetAgentTaskController(controller)

	screen := newClipboardScreen()
	app := newRenderTestApp(t)
	app.screen = screen
	app.runtime = runtime

	liveEvents := make(chan database.TaskEventEntity, 1)
	liveEvents <- taskStreamEvent(taskID, 3, assistant.StreamEvent{
		ToolCallEvent: nil, ToolEvent: nil, Usage: nil,
		Kind: assistant.StreamEventTextDelta, Text: gapTextThree,
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go app.watchAgentTaskEventsWithRuntime(ctx, runtime, taskID, liveEvents, func() {}, true)

	texts := collectAgentTaskStreamTexts(t, screen, 3)

	assert.Equal(t, []string{gapTextOne, gapTextTwo, gapTextThree}, texts)
	assert.Equal(t, 2, controller.eventCalls())
}

func TestAgentTaskSequenceGapReplaysDurableEvents(t *testing.T) {
	t.Parallel()

	const taskID = "gap-task"

	controller := &replayAgentTaskController{
		agentTaskControllerStub: newAgentTaskControllerStub(nil, nil),
		events: []database.TaskEventEntity{
			taskStreamEvent(taskID, 1, assistant.StreamEvent{
				ToolCallEvent: nil, ToolEvent: nil, Usage: nil,
				Kind: assistant.StreamEventTextDelta, Text: "one",
			}),
			taskStreamEvent(taskID, 2, assistant.StreamEvent{
				ToolCallEvent: nil, ToolEvent: nil, Usage: nil,
				Kind: assistant.StreamEventTextDelta, Text: "two",
			}),
		},
	}
	runtime := assistant.NewRuntimeForTest(nil)
	runtime.SetAgentTaskController(controller)

	screen := newClipboardScreen()
	app := newRenderTestApp(t)
	app.screen = screen
	app.runtime = runtime
	live := taskStreamEvent(taskID, 3, assistant.StreamEvent{
		ToolCallEvent: nil, ToolEvent: nil, Usage: nil,
		Kind: assistant.StreamEventTextDelta, Text: "three",
	})

	sequence, terminal := app.forwardAgentTaskEvent(t.Context(), taskID, &live, 0, true)
	assert.Equal(t, int64(3), sequence)
	assert.False(t, terminal)

	texts := collectAgentTaskStreamTexts(t, screen, 3)

	assert.Equal(t, []string{"one", "two", "three"}, texts)
}

func TestAgentTaskGapReplayErrorStopsBeforeOutOfOrderEvent(t *testing.T) {
	t.Parallel()

	controller := &failingReplayAgentTaskController{
		agentTaskControllerStub: newAgentTaskControllerStub(nil, nil),
		err:                     errors.New("database unavailable"),
		calls:                   0,
		mu:                      sync.Mutex{},
	}
	runtime := assistant.NewRuntimeForTest(nil)
	runtime.SetAgentTaskController(controller)

	screen := newClipboardScreen()
	app := newRenderTestApp(t)
	app.screen = screen
	app.runtime = runtime
	live := taskStreamEvent(behaviorTaskID, 2, assistant.StreamEvent{
		ToolCallEvent: nil, ToolEvent: nil, Usage: nil,
		Kind: assistant.StreamEventTextDelta, Text: "out of order",
	})

	sequence, stop := app.forwardAgentTaskEvent(t.Context(), behaviorTaskID, &live, 0, true)
	assert.Zero(t, sequence)
	assert.True(t, stop)

	select {
	case event := <-screen.EventQ():
		interrupt, ok := event.(*tcell.EventInterrupt)
		require.True(t, ok)
		payload, ok := interrupt.Data().(*asyncEvent)
		require.True(t, ok)
		assert.Equal(t, asyncEventAgentTaskReplayError, payload.Kind)
	case <-time.After(time.Second):
		require.FailNow(t, "timed out waiting for gap replay error")
	}

	select {
	case event := <-screen.EventQ():
		require.FailNow(t, "out-of-order event was forwarded", event)
	default:
	}
}

func TestAgentTaskReplayQueryErrorIsSurfaced(t *testing.T) {
	t.Parallel()

	controller := &failingReplayAgentTaskController{
		agentTaskControllerStub: newAgentTaskControllerStub(nil, nil),
		err:                     errors.New("database unavailable"),
		calls:                   0,
		mu:                      sync.Mutex{},
	}
	runtime := assistant.NewRuntimeForTest(nil)
	runtime.SetAgentTaskController(controller)

	screen := newClipboardScreen()
	app := newRenderTestApp(t)
	app.screen = screen
	app.runtime = runtime

	events := make(chan database.TaskEventEntity)
	close(events)

	go app.watchAgentTaskEvents(t.Context(), behaviorTaskID, events, func() {}, true)

	select {
	case event := <-screen.EventQ():
		interrupt, ok := event.(*tcell.EventInterrupt)
		require.True(t, ok)
		_, err := app.handleInterrupt(t.Context(), interrupt)
		require.NoError(t, err)
	case <-time.After(time.Second):
		require.FailNow(t, "timed out waiting for replay error")
	}

	require.NotEmpty(t, app.transcript.History)
	message := app.transcript.History[len(app.transcript.History)-1].Content
	assert.Contains(t, message, "failed to replay agent task activity")
	assert.Contains(t, message, "database unavailable")
}

type replayAgentTaskController struct {
	*agentTaskControllerStub
	events []database.TaskEventEntity
}

type failingReplayAgentTaskController struct {
	*agentTaskControllerStub
	err   error
	calls int
	mu    sync.Mutex
}

func (controller *failingReplayAgentTaskController) Events(
	context.Context,
	string,
	int64,
	int,
) ([]database.TaskEventEntity, error) {
	controller.mu.Lock()
	defer controller.mu.Unlock()

	controller.calls++

	return nil, controller.err
}

func (controller *failingReplayAgentTaskController) eventCalls() int {
	controller.mu.Lock()
	defer controller.mu.Unlock()

	return controller.calls
}

type stagedReplayAgentTaskController struct {
	*agentTaskControllerStub
	batches [][]database.TaskEventEntity
	calls   int
	mu      sync.Mutex
}

func (controller *stagedReplayAgentTaskController) Events(
	_ context.Context,
	_ string,
	_ int64,
	_ int,
) ([]database.TaskEventEntity, error) {
	controller.mu.Lock()
	defer controller.mu.Unlock()

	index := controller.calls
	controller.calls++

	if index >= len(controller.batches) {
		return nil, nil
	}

	return controller.batches[index], nil
}

func (controller *stagedReplayAgentTaskController) eventCalls() int {
	controller.mu.Lock()
	defer controller.mu.Unlock()

	return controller.calls
}

func (controller *replayAgentTaskController) Events(
	_ context.Context,
	taskID string,
	after int64,
	limit int,
) ([]database.TaskEventEntity, error) {
	result := make([]database.TaskEventEntity, 0, len(controller.events))
	for _, event := range controller.events {
		if event.TaskID == taskID && event.Sequence > after {
			result = append(result, event)
			if limit > 0 && len(result) == limit {
				break
			}
		}
	}

	return result, nil
}

func collectAgentTaskStreamTexts(
	t *testing.T,
	screen *clipboardScreen,
	count int,
) []string {
	t.Helper()

	texts := make([]string, 0, count)
	for range count {
		select {
		case event := <-screen.EventQ():
			interrupt, ok := event.(*tcell.EventInterrupt)
			require.True(t, ok)
			payload, ok := interrupt.Data().(*asyncEvent)
			require.True(t, ok)
			assert.Equal(t, asyncEventAgentTaskStream, payload.Kind)

			var streamEvent assistant.StreamEvent
			require.NoError(t, json.Unmarshal([]byte(payload.Text), &streamEvent))
			texts = append(texts, streamEvent.Text)
		case <-time.After(time.Second):
			require.FailNow(t, "timed out waiting for agent task stream events")
		}
	}

	return texts
}

func taskStreamEvent(taskID string, sequence int64, event assistant.StreamEvent) database.TaskEventEntity {
	payload, err := json.Marshal(event)
	if err != nil {
		panic(err)
	}

	return database.TaskEventEntity{
		TaskID: taskID, Sequence: sequence,
		Event: database.EventEntity{
			CreatedAt: time.Time{}, ID: "", Kind: string(event.Kind), PayloadJSON: string(payload),
		},
	}
}

func taskStreamPayload(t *testing.T, event assistant.StreamEvent) string {
	t.Helper()

	payload, err := json.Marshal(event)
	require.NoError(t, err)

	return string(payload)
}

func TestAgentTaskReplayErrorDoesNotImmediatelyRestartFailedWatch(t *testing.T) {
	t.Parallel()

	task := behaviorAgentTask(behaviorTaskID, database.TaskRunning)
	controller := &failingReplayAgentTaskController{
		agentTaskControllerStub: newAgentTaskControllerStub(
			map[string]*database.AgentTaskEntity{task.Task.ID: &task},
			[]database.AgentTaskEntity{task},
		),
		err:   errors.New("database unavailable"),
		calls: 0,
		mu:    sync.Mutex{},
	}
	runtime := assistant.NewRuntimeForTest(nil)
	runtime.SetAgentTaskController(controller)

	screen := newClipboardScreen()
	app := newRenderTestApp(t)
	app.screen = screen
	app.runtime = runtime
	app.sessionID = task.Task.OwnerSessionID
	app.agentTasks = []database.AgentTaskEntity{task}
	watchCanceled := false
	app.agentTaskWatches[task.Task.ID] = func() { watchCanceled = true }

	events := make(chan database.TaskEventEntity)
	go app.watchAgentTaskEventsWithRuntime(
		t.Context(), runtime, task.Task.ID, events, func() {}, true,
	)

	select {
	case event := <-screen.EventQ():
		interrupt, ok := event.(*tcell.EventInterrupt)
		require.True(t, ok)
		_, err := app.handleInterrupt(t.Context(), interrupt)
		require.NoError(t, err)
	case <-time.After(time.Second):
		require.FailNow(t, "timed out waiting for replay error")
	}

	assert.True(t, watchCanceled)
	assert.Equal(t, 1, controller.eventCalls())
	assert.NotContains(t, app.agentTaskWatches, task.Task.ID)

	select {
	case event := <-screen.EventQ():
		require.FailNow(t, "failed watch restarted immediately", event)
	case <-time.After(25 * time.Millisecond):
	}
}

func TestOrdinaryAgentTaskWatchReplaysPersistedTerminalEvent(t *testing.T) {
	t.Parallel()

	const taskID = "persisted-terminal"

	controller := &replayAgentTaskController{
		agentTaskControllerStub: newAgentTaskControllerStub(nil, nil),
		events: []database.TaskEventEntity{{
			TaskID: taskID, Sequence: 1,
			Event: database.EventEntity{
				CreatedAt: time.Time{}, ID: "", Kind: agentTaskSucceededEvent, PayloadJSON: `{}`,
			},
		}},
	}
	runtime := assistant.NewRuntimeForTest(nil)
	runtime.SetAgentTaskController(controller)

	screen := newClipboardScreen()
	app := newRenderTestApp(t)
	app.screen = screen
	app.runtime = runtime

	events := make(chan database.TaskEventEntity)
	go app.watchAgentTaskEvents(t.Context(), taskID, events, func() {}, true)

	select {
	case event := <-screen.EventQ():
		interrupt, ok := event.(*tcell.EventInterrupt)
		require.True(t, ok)
		payload, ok := interrupt.Data().(*asyncEvent)
		require.True(t, ok)
		assert.Equal(t, asyncEventAgentTaskChanged, payload.Kind)
		assert.Equal(t, taskID, payload.Text)
	case <-time.After(time.Second):
		require.FailNow(t, "timed out waiting for persisted terminal event")
	}
}

func TestAgentTaskSequenceGapReplaysTerminalInOrder(t *testing.T) {
	t.Parallel()

	const taskID = "terminal-gap"

	controller := &replayAgentTaskController{
		agentTaskControllerStub: newAgentTaskControllerStub(nil, nil),
		events: []database.TaskEventEntity{
			taskStreamEvent(taskID, 1, assistant.StreamEvent{
				ToolCallEvent: nil, ToolEvent: nil, Usage: nil,
				Kind: assistant.StreamEventTextDelta, Text: "before completion",
			}),
			{
				TaskID: taskID, Sequence: 2,
				Event: database.EventEntity{
					CreatedAt: time.Time{}, ID: "", Kind: agentTaskSucceededEvent, PayloadJSON: `{}`,
				},
			},
		},
	}
	runtime := assistant.NewRuntimeForTest(nil)
	runtime.SetAgentTaskController(controller)

	screen := newClipboardScreen()
	app := newRenderTestApp(t)
	app.screen = screen
	app.runtime = runtime
	live := controller.events[1]

	sequence, terminal := app.forwardAgentTaskEvent(t.Context(), taskID, &live, 0, true)
	assert.Equal(t, int64(2), sequence)
	assert.True(t, terminal)

	kinds := make([]asyncEventKind, 0, 2)

	for range 2 {
		select {
		case event := <-screen.EventQ():
			interrupt, ok := event.(*tcell.EventInterrupt)
			require.True(t, ok)
			payload, ok := interrupt.Data().(*asyncEvent)
			require.True(t, ok)

			kinds = append(kinds, payload.Kind)
		case <-time.After(time.Second):
			require.FailNow(t, "timed out waiting for terminal gap replay")
		}
	}

	assert.Equal(t, []asyncEventKind{asyncEventAgentTaskStream, asyncEventAgentTaskChanged}, kinds)
}

func TestUnexpectedAgentTaskWatchClosureClearsRegistration(t *testing.T) {
	t.Parallel()

	const taskID = "closed-watch"

	controller := &replayAgentTaskController{
		agentTaskControllerStub: newAgentTaskControllerStub(nil, nil),
		events:                  nil,
	}
	runtime := assistant.NewRuntimeForTest(nil)
	runtime.SetAgentTaskController(controller)

	screen := newClipboardScreen()
	app := newRenderTestApp(t)
	app.screen = screen
	app.runtime = runtime
	canceled := false
	app.agentTaskWatches[taskID] = func() { canceled = true }

	events := make(chan database.TaskEventEntity)
	close(events)

	go app.watchAgentTask(t.Context(), taskID, events, func() {})

	select {
	case event := <-screen.EventQ():
		interrupt, ok := event.(*tcell.EventInterrupt)
		require.True(t, ok)
		_, err := app.handleInterrupt(t.Context(), interrupt)
		require.NoError(t, err)
	case <-time.After(time.Second):
		require.FailNow(t, "timed out waiting for closed-watch reconciliation")
	}

	assert.True(t, canceled)
	assert.NotContains(t, app.agentTaskWatches, taskID)
	assert.Contains(t, app.transcript.History[len(app.transcript.History)-1].Content, "event stream closed")
}

func TestWatchAgentTaskPostsTerminalChange(t *testing.T) {
	t.Parallel()

	const taskID = "task-1"

	screen := newClipboardScreen()
	app := newRenderTestApp(t)
	app.screen = screen

	events := make(chan database.TaskEventEntity, 1)
	cancelCalled := make(chan struct{}, 1)

	events <- database.TaskEventEntity{
		Event: database.EventEntity{
			CreatedAt:   time.Time{},
			ID:          "event-1",
			Kind:        agentTaskSucceededEvent,
			PayloadJSON: `{}`,
		},
		TaskID:   taskID,
		Sequence: 4,
	}

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	go app.watchAgentTask(ctx, taskID, events, func() { cancelCalled <- struct{}{} })

	var event tcell.Event
	select {
	case event = <-screen.EventQ():
	case <-ctx.Done():
		require.FailNow(t, "watch did not post terminal task event")
	}

	interrupt, ok := event.(*tcell.EventInterrupt)
	require.True(t, ok)
	payload, ok := interrupt.Data().(*asyncEvent)
	require.True(t, ok)
	assert.Equal(t, asyncEventAgentTaskChanged, payload.Kind)
	assert.Equal(t, taskID, payload.Text)

	select {
	case <-cancelCalled:
	case <-ctx.Done():
		require.FailNow(t, "watch did not cancel its subscription")
	}
}
