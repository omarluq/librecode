package terminal

import (
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/transcript"
)

func TestTabFocusesAgentTaskSummaryBeforeTranscriptList(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.activeWorkflows = []database.WorkflowRunEntity{workflowSummaryRun(toolDisplayWorkflow, database.TaskRunning)}
	app.agentTasks = []database.AgentTaskEntity{
		behaviorAgentTask("one", database.TaskRunning),
		behaviorAgentTask("two", database.TaskRunning),
	}
	app.appendMessage(newChatMessage(transcript.RoleAssistant, "- transcript item"))

	pressTerminalKey(t, app, tcell.KeyTab, "")
	require.True(t, app.agentTaskSummaryFocused())
	assert.False(t, app.transcriptListFocused())
	assert.Equal(t, focusKindAgentTaskSummary, app.focusState().Kind)
	assert.Equal(t, 0, app.agentTaskSummarySelection.ItemIndex)

	pressTerminalKey(t, app, tcell.KeyDown, "")
	assert.Equal(t, 1, app.agentTaskSummarySelection.ItemIndex)
	pressTerminalKey(t, app, tcell.KeyPgDn, "")
	assert.Equal(t, 2, app.agentTaskSummarySelection.ItemIndex)

	lines := app.renderAgentTaskSummary(80)
	assert.Equal(t, app.theme.selected(), lines[2].Style)

	pressTerminalKey(t, app, tcell.KeyTab, "")
	assert.False(t, app.agentTaskSummaryFocused())
	assert.Equal(t, focusKindComposer, app.focusState().Kind)
}

func TestAgentTaskSummaryEnterOnWorkflowExpandsInline(t *testing.T) {
	t.Parallel()

	run := workflowSummaryRun(toolDisplayWorkflow, database.TaskRunning)
	app := newRenderTestApp(t)
	app.sessionID = workflowTestSessionID
	app.workflows = &workflowInspectorStub{
		listErr: nil, getErr: nil, eventsErr: nil, agentTasksErr: nil,
		getRun: &run, runs: nil, events: nil, children: nil, details: nil, found: true,
	}
	app.activeWorkflows = []database.WorkflowRunEntity{run}
	app.agentTasks = []database.AgentTaskEntity{behaviorAgentTask("top-level", database.TaskRunning)}
	transcriptBefore := app.transcript

	pressTerminalKey(t, app, tcell.KeyTab, "")
	collapsedLayout := app.defaultRuntimeLayout(80, 24)
	collapsedLines := app.renderAgentTaskSummary(80)
	require.Len(t, collapsedLines, 3)
	assert.Equal(t, pendingToolIndicator+" "+app.workflowSummaryLabel(&app.activeWorkflows[0]), collapsedLines[0].Text)
	assert.Empty(t, collapsedLines[2].Text)
	pressTerminalKey(t, app, tcell.KeyEnter, "")

	assert.True(t, app.agentTaskSummaryFocused())
	assert.Equal(t, modeChat, app.mode)
	assert.Equal(t, run.Task.ID, app.workflowSummaryRunID)
	assert.Empty(t, app.workflowPanelRunID)
	assert.Nil(t, app.panel)
	assert.Equal(t, transcriptBefore, app.transcript)

	lines := app.renderAgentTaskSummary(80)
	require.Len(t, lines, 4)
	assert.Contains(t, lines[0].Text, "Workflow:")
	assert.Contains(t, lines[1].Text, "STEP")

	expandedLayout := app.defaultRuntimeLayout(80, 24)
	assert.NotEqual(t, collapsedLayout.Transcript, expandedLayout.Transcript)
	assert.NotEqual(t, collapsedLayout.Composer, expandedLayout.Composer)
	assert.NotEqual(t, collapsedLayout.Status, expandedLayout.Status)

	pressTerminalKey(t, app, tcell.KeyEscape, "")
	assert.Empty(t, app.workflowSummaryRunID)
	assert.True(t, app.agentTaskSummaryFocused())
	assert.Equal(t, transcriptBefore, app.transcript)

	lines = app.renderAgentTaskSummary(80)
	require.Len(t, lines, 3)
	assert.Equal(t, pendingToolIndicator+" "+app.workflowSummaryLabel(&app.activeWorkflows[0]), lines[0].Text)
	assert.NotContains(t, lines[0].Text, "STEP")
}

func TestValidateAgentTaskSummarySelectionClampsNegativeIndex(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.agentTasks = []database.AgentTaskEntity{behaviorAgentTask("top-level", database.TaskRunning)}
	app.agentTaskSummarySelection = agentTaskSummarySelection{ItemIndex: -2, Active: true}

	require.True(t, app.validateAgentTaskSummarySelection())
	assert.True(t, app.agentTaskSummaryFocused())
	assert.Equal(t, 0, app.agentTaskSummarySelection.ItemIndex)
}

func TestSelectedAgentTaskSummaryTaskIDSkipsWorkflowsAndChildTasks(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.activeWorkflows = []database.WorkflowRunEntity{workflowSummaryRun(toolDisplayWorkflow, database.TaskRunning)}
	child := behaviorAgentTask("child", database.TaskRunning)
	child.Task.ParentTaskID = toolDisplayWorkflow
	app.agentTasks = []database.AgentTaskEntity{
		child,
		behaviorAgentTask("top-level", database.TaskRunning),
	}
	app.agentTaskSummarySelection = agentTaskSummarySelection{ItemIndex: 1, Active: true}

	taskID, ok := app.selectedAgentTaskSummaryTaskID()
	require.True(t, ok)
	assert.Equal(t, "top-level", taskID)
}

func TestTranscriptListFocusAndNavigation(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.transcript.LastMaxRows = 10
	app.appendMessage(newChatMessage(transcript.RoleAssistant, "- one\n- two\n- three\n- four\n- five\n- six"))

	pressTerminalKey(t, app, tcell.KeyTab, "")
	require.True(t, app.transcriptListFocused())
	assert.Equal(t, focusKindTranscriptList, app.focusState().Kind)
	assert.Equal(t, 0, app.transcriptList.ItemIndex)

	pressTerminalKey(t, app, tcell.KeyDown, "")
	assert.Equal(t, 1, app.transcriptList.ItemIndex)
	pressTerminalKey(t, app, tcell.KeyPgDn, "")
	assert.Equal(t, 5, app.transcriptList.ItemIndex)
	pressTerminalKey(t, app, tcell.KeyDown, "")
	assert.Equal(t, 5, app.transcriptList.ItemIndex)
	pressTerminalKey(t, app, tcell.KeyPgUp, "")
	assert.Equal(t, 0, app.transcriptList.ItemIndex)
	pressTerminalKey(t, app, tcell.KeyUp, "")
	assert.Equal(t, 0, app.transcriptList.ItemIndex)

	pressTerminalKey(t, app, tcell.KeyTab, "")
	assert.False(t, app.transcriptListFocused())
	assert.Equal(t, focusKindComposer, app.focusState().Kind)
}

func TestTranscriptListSelectsLatestEligibleAssistantMessage(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.appendMessage(newChatMessage(transcript.RoleAssistant, "- old"))
	app.appendMessage(newChatMessage(transcript.RoleUser, "- user list"))
	app.appendMessage(newChatMessage(transcript.RoleAssistant, "no list"))

	assert.True(t, app.focusLatestTranscriptList())
	assert.Equal(t, 0, app.transcriptList.MessageIndex)
}

func TestTranscriptListNoListAndLifecycle(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.appendMessage(newChatMessage(transcript.RoleAssistant, "plain text"))
	assert.False(t, app.focusLatestTranscriptList())

	app.appendMessage(newChatMessage(transcript.RoleAssistant, "- selectable"))
	require.True(t, app.focusLatestTranscriptList())
	app.appendMessage(newChatMessage(transcript.RoleAssistant, "new content"))
	assert.False(t, app.transcriptListFocused())

	require.True(t, app.focusLatestTranscriptList())
	app.truncateMessages(1)
	assert.False(t, app.transcriptListFocused())
}

func TestTranscriptListHighlightDoesNotMutateCache(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.appendMessage(newChatMessage(transcript.RoleAssistant, "- alpha beta gamma\n- second"))

	width := 12
	base := app.transcript.LineCache.lines(app, width, 0)
	baseStyle := base[1].Style

	require.True(t, app.focusLatestTranscriptList())
	styled := app.cachedMessageLines(width, 0)
	assert.Equal(t, app.theme.selected(), styled[1].Style)
	assert.Equal(t, app.theme.selected(), styled[2].Style)
	assert.Equal(t, baseStyle, app.transcript.LineCache.items[0].Lines[1].Style)
	assert.NotEqual(t, app.theme.selected(), styled[len(styled)-2].Style)
}
