package terminal

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/testutil"
	"github.com/omarluq/librecode/internal/tool"
	"github.com/omarluq/librecode/internal/transcript"
)

type runningToolBlockTestCase struct {
	run  func(t *testing.T, app *App)
	name string
	want []string
}

func TestAgentManagementToolsUseTaskSummaryInsteadOfToolBlocks(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	call := testToolCallEvent(agentStartToolName, `{"agent":"explore","prompt":"review"}`)

	app.applyStreamedToolStart(&call, "")
	app.applyStreamedToolEvent(&assistant.ToolEvent{
		CallID: "", ParentCallID: "", Sequence: 0,
		Name: agentStartToolName, ArgumentsJSON: call.ArgumentsJSON, DetailsJSON: "",
		Result: "started", Error: "", IsError: false,
	})

	assert.Empty(t, app.runningToolBlocks)
	assert.Empty(t, app.transcript.Streaming.Blocks)
}

func TestRenderAgentTaskSummary(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	task := testAgentTask(database.TaskRunning)
	task.Prompt = "review the code\nfor concurrency issues"
	app.agentTasks = []database.AgentTaskEntity{task}

	lines := app.renderAgentTaskSummary(80)
	require.Len(t, lines, 2)
	assert.Equal(t, "◌ explore(review the code for concurrency issues)", lines[0].Text)
	require.Len(t, lines[0].Spans, 2)
	assert.Equal(t, pendingToolIndicator, lines[0].Spans[0].Text)
	assert.Equal(t, " explore(review the code for concurrency issues)", lines[0].Spans[1].Text)
	assert.Equal(t, defaultWorkingShimmerBrightColor(), lines[0].Spans[0].Style.GetForeground())
	assert.Equal(t, app.theme.colors[colorMuted], lines[0].Spans[1].Style.GetForeground())
	assert.Empty(t, lines[len(lines)-1].Text)
}

func TestRenderAgentTaskSummaryTruncatesPromptToOneLine(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	task := testAgentTask(database.TaskRunning)
	task.Prompt = strings.Repeat("investigate ", 10)
	app.agentTasks = []database.AgentTaskEntity{task}

	lines := app.renderAgentTaskSummary(30)
	require.Len(t, lines, 2)
	assert.Equal(t, "◌ explore(investigate investi…", lines[0].Text)
	assert.NotContains(t, lines[0].Text, "\n")
}

func TestAgentTaskSummaryRendersBelowComposer(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.agentTasks = []database.AgentTaskEntity{testAgentTask(database.TaskRunning)}

	layout := app.composerLayout(80, 24)
	require.GreaterOrEqual(t, len(layout.footerLines), 2)
	assert.Contains(t, layout.footerLines[0].Text, "explore")
	assert.Equal(t, layout.editorStart+len(layout.editor.Lines), layout.footerStart)

	dynamicLines := flattenStyledLineGroups(app.dynamicMessageLineGroups(80), 100)
	assert.NotContains(t, strings.Join(lineTexts(dynamicLines), "\n"), "explore(")
}

func TestRenderAgentTaskSummaryUsesStaticIndicator(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.agentTasks = []database.AgentTaskEntity{testAgentTask(database.TaskRunning)}

	app.workFrame = 0
	first := app.renderAgentTaskSummary(80)
	app.workFrame = 3
	later := app.renderAgentTaskSummary(80)

	require.NotEmpty(t, first)
	require.NotEmpty(t, later)
	assert.Equal(t, pendingToolIndicator, first[0].Spans[0].Text)
	assert.Equal(t, first[0].Text, later[0].Text)
}

func TestAgentTaskCompletion(t *testing.T) {
	t.Parallel()

	const taskID = "task-1"

	task := testAgentTask(database.TaskSucceeded)
	task.Task.ID = taskID
	task.Task.Result = "review complete"

	completion, completed := agentTaskCompletion(database.TaskRunning, &task)

	require.True(t, completed)
	assert.Contains(t, completion, "Agent explore (task-1) finished with state succeeded")
	assert.Contains(t, completion, "review complete")
}

func TestAgentTaskCompletionIgnoresAlreadyTerminalTask(t *testing.T) {
	t.Parallel()

	const taskID = "task-1"

	task := testAgentTask(database.TaskSucceeded)
	task.Task.ID = taskID

	completion, completed := agentTaskCompletion(database.TaskSucceeded, &task)

	assert.False(t, completed)
	assert.Empty(t, completion)
}

func TestAgentTaskCompletionEventDrawsCollapsedExpandableToolResult(t *testing.T) {
	t.Parallel()

	const taskID = "task-1"

	app := newRenderTestApp(t)
	app.working = true
	app.activePrompt = &activePromptState{
		Cancel: func() {}, ParentEntryID: nil, SessionID: "", UserEntryID: "", Prompt: "", ID: 1, Canceled: false,
	}
	app.scrollOffset = 10
	app.agentTasks = []database.AgentTaskEntity{testAgentTask(database.TaskRunning)}
	app.agentTasks[0].Task.ID = taskID
	completion := "Agent explore finished.\n\n" + strings.Repeat("result line\n", 10)

	app.handlePromptAsyncEvent(context.Background(), &asyncEvent{
		Response: nil, ToolCallEvent: nil, ToolEvent: nil, Usage: nil,
		Kind: asyncEventAgentTaskCompleted, Provider: taskID, Text: completion, PromptID: 0,
	})

	require.Len(t, app.liveAgentCompletions, 1)
	message := app.liveAgentCompletions[0]
	assert.Equal(t, transcript.RoleToolResult, message.Role)
	assert.Equal(t, 10, app.scrollOffset)
	assert.Empty(t, app.agentTasks)

	collapsed := app.renderToolMessage(80, message)
	assert.NotEqual(t, -1, lineIndexContaining(collapsed, "earlier output"))
	assert.Equal(t, -1, lineIndexContaining(collapsed, "Agent explore finished."))

	app.toolsExpanded = true
	expanded := app.renderToolMessage(80, message)
	assert.NotEqual(t, -1, lineIndexContaining(expanded, "Agent explore finished."))

	app.handlePromptAsyncEvent(context.Background(), &asyncEvent{
		Response: nil, ToolCallEvent: nil, ToolEvent: nil, Usage: nil,
		Kind: asyncEventAgentTaskCompleted, Provider: taskID, Text: completion, PromptID: 0,
	})
	assert.Len(t, app.liveAgentCompletions, 1)
}

func TestAgentCompletionRendersCollapsedInLiveTail(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	content := formatAgentCompletionForUI("Agent explore finished.\n\n" + strings.Repeat("result line\n", 10))
	app.addAgentCompletionMessage(content)

	collapsed := strings.Join(lineTexts(flattenStyledLineGroups(app.dynamicMessageLineGroups(80), 100)), "\n")
	assert.Contains(t, collapsed, "earlier output")
	assert.NotContains(t, collapsed, "Agent explore finished.")

	app.toolsExpanded = true
	expanded := strings.Join(lineTexts(flattenStyledLineGroups(app.dynamicMessageLineGroups(80), 100)), "\n")
	assert.Contains(t, expanded, "Agent explore finished.")
}

func TestAddAgentCompletionMessageUsesLiveTailWhenIdle(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	content := formatAgentCompletionForUI("Agent explore finished.\n\nreview complete")

	app.addAgentCompletionMessage(content)

	require.Len(t, app.liveAgentCompletions, 1)
	assert.Equal(t, transcript.RoleToolResult, app.liveAgentCompletions[0].Role)
	assert.Empty(t, app.transcript.History)
	assert.Empty(t, app.transcript.Streaming.Blocks)
}

func TestAgentCompletionSurvivesPromptStreamingReset(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.activePrompt = &activePromptState{
		Cancel: func() {}, ParentEntryID: nil, SessionID: "", UserEntryID: "", Prompt: "", ID: 1, Canceled: false,
	}
	content := formatAgentCompletionForUI("Agent explore finished.\n\nreview complete")
	app.addAgentCompletionMessage(content)
	app.appendStreamingBlock(transcript.RoleAssistant, "parent response")

	app.applyPromptResponse(context.Background(), &assistant.PromptResponse{
		Usage: model.EmptyTokenUsage(), SessionID: "session-1", UserEntryID: "", AssistantEntryID: "",
		Text: "parent response", Thinking: nil, ToolEvents: nil, Cached: false,
	}, 1)

	assert.Empty(t, app.liveAgentCompletions)
	require.Len(t, app.transcript.History, 2)
	assert.Equal(t, transcript.RoleToolResult, app.transcript.History[0].Role)
	assert.Equal(t, content, app.transcript.History[0].Content)
	assert.Equal(t, transcript.RoleAssistant, app.transcript.History[1].Role)
}

func TestAgentCompletionStaysLiveAcrossQueuedContinuation(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.activePrompt = &activePromptState{
		Cancel: func() {}, ParentEntryID: nil, SessionID: "", UserEntryID: "", Prompt: "", ID: 1, Canceled: false,
	}
	content := formatAgentCompletionForUI("Agent explore finished.\n\nreview complete")
	app.addAgentCompletionMessage(content)
	app.queuePrompt("continue with the agent result", false)

	app.finishPrompt()
	app.commitLiveAgentCompletions()

	require.Len(t, app.liveAgentCompletions, 1)
	assert.Equal(t, content, app.liveAgentCompletions[0].Content)
	assert.Equal(t, transcript.RoleToolResult, app.liveAgentCompletions[0].Role)
}

func testAgentTask(state database.TaskState) database.AgentTaskEntity {
	return database.AgentTaskEntity{
		Task: database.TaskEntity{
			CreatedAt: time.Time{}, StartedAt: nil, FinishedAt: nil, UpdatedAt: time.Time{},
			LeaseExpiresAt: nil, ID: "", Kind: database.TaskKindAgent, ParentTaskID: "",
			OwnerSessionID: "", ConcurrencyKey: "", LeaseOwner: "",
			State: state, Result: "", ErrorCode: "", ErrorMessage: "",
		},
		ChildSessionID: "", AgentName: "explore", Prompt: "", Model: "", Provider: "",
		PolicyJSON: "{}", UsageJSON: "{}", Depth: 0,
	}
}

func TestRunningToolBlocks(t *testing.T) {
	t.Parallel()

	for _, tt := range runningToolBlockTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runRunningToolBlockCase(t, tt)
		})
	}
}

func runningToolBlockTestCases() []runningToolBlockTestCase {
	return []runningToolBlockTestCase{
		runningToolAppendRenderRemoveCase(),
		runningToolFallbackNameCase(),
		runningToolRemoveByNameAndArgumentsCase(),
		runningToolRemoveByNameFallbackCase(),
		runningToolResetCase(),
	}
}

func runningToolAppendRenderRemoveCase() runningToolBlockTestCase {
	bashTestArguments := `{"command":"go test ./..."}`

	return runningToolBlockTestCase{
		run: func(t *testing.T, app *App) {
			t.Helper()

			call := testToolCallEvent(testToolBash, bashTestArguments)
			call.Arguments = testutil.ToolArguments(map[string]any{testToolCommandKey: "go test ./..."})

			app.applyStreamedToolStart(&call, "")
			require.NotEmpty(t, app.runningToolBlocks)

			lines := app.renderRunningToolBlock(80, &app.runningToolBlocks[0].Call)
			assert.NotEqual(t, -1, lineIndexContaining(lines, "◌ $ go test ./..."))

			app.applyStreamedToolEvent(&assistant.ToolEvent{
				CallID:        "",
				ParentCallID:  "",
				Sequence:      0,
				Name:          testToolBash,
				ArgumentsJSON: bashTestArguments,
				DetailsJSON:   "",
				Result:        "ok",
				Error:         "",
				IsError:       false,
			})
		},
		name: "append render and remove completed tool",
		want: []string{},
	}
}

func runningToolFallbackNameCase() runningToolBlockTestCase {
	return runningToolBlockTestCase{
		run: func(t *testing.T, app *App) {
			t.Helper()

			call := testToolCallEvent("", `{"command":"go test"}`)
			app.applyStreamedToolStart(&call, testToolBash)
		},
		name: "use fallback for blank streamed tool name",
		want: []string{testToolBash},
	}
}

func TestApplyStreamedToolStartSkipsAgentFallbackName(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.applyStreamedToolStart(nil, agentStartToolName)

	assert.Empty(t, app.runningToolBlocks)
}

func TestRemoveRunningToolBlockPrefersCallID(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	first := testRunningToolBlock(testToolRead, `{"path":"same.go"}`)
	first.Call.ID = "call-alpha"
	second := testRunningToolBlock(testToolRead, `{"path":"same.go"}`)
	second.Call.ID = "call-beta"
	app.runningToolBlocks = []runningToolBlock{first, second}

	app.removeRunningToolBlock(&assistant.ToolEvent{
		CallID: "call-beta", ParentCallID: "outer", Name: testToolRead, ArgumentsJSON: `{"path":"same.go"}`,
		DetailsJSON: "", Result: "ok", Error: "", Sequence: 2, IsError: false,
	})

	require.Len(t, app.runningToolBlocks, 1)
	assert.Equal(t, "call-alpha", app.runningToolBlocks[0].Call.ID)
}

func runningToolRemoveByNameAndArgumentsCase() runningToolBlockTestCase {
	sharedArguments := `{"path":"same.go"}`

	return runningToolBlockTestCase{
		run: func(t *testing.T, app *App) {
			t.Helper()

			app.runningToolBlocks = []runningToolBlock{
				testRunningToolBlock(testToolRead, sharedArguments),
				testRunningToolBlock(testToolWrite, sharedArguments),
			}
			app.removeRunningToolBlock(&assistant.ToolEvent{
				CallID:        "",
				ParentCallID:  "",
				Sequence:      0,
				Name:          testToolWrite,
				ArgumentsJSON: sharedArguments,
				DetailsJSON:   "",
				Result:        "",
				Error:         "",
				IsError:       false,
			})
		},
		name: "remove by matching name and arguments",
		want: []string{testToolRead},
	}
}

func runningToolRemoveByNameFallbackCase() runningToolBlockTestCase {
	return runningToolBlockTestCase{
		run: func(t *testing.T, app *App) {
			t.Helper()

			app.runningToolBlocks = []runningToolBlock{
				testRunningToolBlock(testToolRead, `{"path":"a.go"}`),
				testRunningToolBlock(testToolBash, `{"command":"go test"}`),
			}
			app.removeRunningToolBlock(&assistant.ToolEvent{
				CallID:        "",
				ParentCallID:  "",
				Sequence:      0,
				Name:          testToolBash,
				ArgumentsJSON: "",
				DetailsJSON:   "",
				Result:        "",
				Error:         "",
				IsError:       false,
			})
		},
		name: "remove falls back to name when arguments are missing",
		want: []string{testToolRead},
	}
}

func runningToolResetCase() runningToolBlockTestCase {
	return runningToolBlockTestCase{
		run: func(t *testing.T, app *App) {
			t.Helper()

			app.runningToolBlocks = []runningToolBlock{testRunningToolBlock(testToolBash, "")}
			app.resetStreamingBlocks()
		},
		name: "reset clears running tools",
		want: []string{},
	}
}

func runRunningToolBlockCase(t *testing.T, testCase runningToolBlockTestCase) {
	t.Helper()

	app := newRenderTestApp(t)
	testCase.run(t, app)

	assert.Equal(t, testCase.want, runningToolBlockNames(app.runningToolBlocks))
}

func testRunningToolBlock(name, argumentsJSON string) runningToolBlock {
	return runningToolBlock{
		StartedAt: time.Time{},
		Call:      testToolCallEvent(name, argumentsJSON),
	}
}

func testToolCallEvent(name, argumentsJSON string) assistant.ToolCallEvent {
	return assistant.ToolCallEvent{
		Arguments:     tool.EmptyArguments(),
		ID:            "",
		ParentCallID:  "",
		Name:          name,
		ArgumentsJSON: argumentsJSON,
		Sequence:      0,
	}
}

func runningToolBlockNames(blocks []runningToolBlock) []string {
	names := make([]string, 0, len(blocks))
	for _, block := range blocks {
		names = append(names, block.Call.Name)
	}

	return names
}
