package terminal

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/tui"
)

const (
	completedSummaryElapsed = "1m24s"
	summaryMetricFirstItem  = "one"
	summaryMetricSecondItem = "two"
	summaryWorkflowID       = "summary-workflow"
	summaryWorkflowChildID  = "summary-workflow-child"
	summaryInspectedTaskID  = "inspected"
)

func TestSummaryElapsedExcludesQueueTime(t *testing.T) {
	t.Parallel()

	created := time.Unix(10, 0)
	started := time.Unix(100, 0)

	finished := time.Unix(184, 0)
	for _, test := range []struct {
		name     string
		state    database.TaskState
		started  *time.Time
		finished *time.Time
		want     string
	}{
		{name: asyncTestQueuedText, state: database.TaskQueued, started: nil, finished: nil, want: asyncTestQueuedText},
		{name: "running", state: database.TaskRunning, started: &started, finished: nil, want: "42s"},
		{
			name: "succeeded", state: database.TaskSucceeded,
			started: &started, finished: &finished, want: completedSummaryElapsed,
		},
		{
			name: "failed", state: database.TaskFailed,
			started: &started, finished: &finished, want: completedSummaryElapsed,
		},
		{
			name: "canceled", state: database.TaskCanceled,
			started: &started, finished: &finished, want: completedSummaryElapsed,
		},
		{
			name: "interrupted", state: database.TaskInterrupted,
			started: &started, finished: &finished, want: completedSummaryElapsed,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			task := database.TaskEntity{
				CreatedAt: created, StartedAt: test.started, FinishedAt: test.finished, UpdatedAt: time.Time{},
				LeaseExpiresAt: nil, ID: "", Kind: "", ParentTaskID: "", OwnerSessionID: "",
				ConcurrencyKey: "", LeaseOwner: "", State: test.state, Result: "", ErrorCode: "", ErrorMessage: "",
			}
			assert.Equal(t, test.want, summaryElapsed(&task, time.Unix(142, 0)))
		})
	}
}

func TestAgentSummaryMetricsAndSuffixPreservingWidth(t *testing.T) {
	t.Parallel()

	started := time.Unix(100, 0)
	task := testAgentTask(database.TaskRunning)
	task.Task.StartedAt = &started
	task.Prompt = strings.Repeat("界e\u0301", 20)
	task.UsageJSON = `{"input_tokens":8000,"output_tokens":700,"reported":true}`

	label := agentTaskSummaryLabelForWidth(&task, time.Unix(142, 0), 44)
	assert.Contains(t, label, ") · 42s · 8.7k tok")
	assert.LessOrEqual(t, tui.Width(label), 44)
	assert.Contains(t, label, "…)")

	fullLabel := agentTaskSummaryLabelForWidth(&task, time.Unix(142, 0), 200)
	assert.Equal(t, "explore("+task.Prompt+") · 42s · 8.7k tok", fullLabel)
}

func TestSummaryMetricWidthFallbacks(t *testing.T) {
	t.Parallel()

	started := time.Unix(100, 0)
	task := testAgentTask(database.TaskRunning)
	task.Task.StartedAt = &started
	task.Prompt = strings.Repeat("investigate ", 10)
	task.UsageJSON = `{"input_tokens":8000,"output_tokens":700,"reported":true}`

	for _, test := range []struct {
		name          string
		wantContains  string
		wantOmissions []string
		width         int
	}{
		{name: "full metrics", wantContains: " · 42s · 8.7k tok", wantOmissions: []string{}, width: 44},
		{name: "tokens omitted", wantContains: ") · 42s", wantOmissions: []string{"tok"}, width: 25},
		{name: "metrics omitted", wantContains: ")", wantOmissions: []string{"42s", "tok"}, width: 14},
		{name: "zero width", wantContains: "", wantOmissions: []string{agentDefaultDisplayName}, width: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			label := agentTaskSummaryLabelForWidth(&task, time.Unix(142, 0), test.width)
			assert.Contains(t, label, test.wantContains)
			assert.LessOrEqual(t, tui.Width(label), test.width)

			for _, omitted := range test.wantOmissions {
				assert.NotContains(t, label, omitted)
			}
		})
	}
}

func TestWorkflowSummaryAggregatesDistinctFinalUsage(t *testing.T) {
	t.Parallel()

	first := testAgentTask(database.TaskSucceeded)
	first.Task.ID = summaryMetricFirstItem
	first.UsageJSON = `{"input_tokens":10000,"output_tokens":2400,"reported":true}`
	second := testAgentTask(database.TaskRunning)
	second.Task.ID = summaryMetricSecondItem
	second.UsageJSON = `{}`
	emptyLink := database.WorkflowAgentTaskEntity{
		CreatedAt: time.Time{}, WorkflowTaskID: "", AgentTaskID: "", NodeKey: "",
		InvocationIndex: 0, Sequence: 0,
	}
	details := []database.WorkflowAgentTaskDetail{
		{AgentTask: first, Link: emptyLink},
		{AgentTask: first, Link: emptyLink},
		{AgentTask: second, Link: emptyLink},
	}

	terminal, total, usageTotals := workflowSummaryMetricsWithLive(details, nil)
	require.Equal(t, 1, terminal)
	require.Equal(t, 2, total)
	assert.Equal(t, "12k+ tok", usageTotalsSuffix(usageTotals, total))
}

func TestDecodeFinalUsageTotalsDistinguishesUnknownAndReportedZero(t *testing.T) {
	t.Parallel()

	_, known := decodeFinalUsageTotals(`{}`)
	assert.False(t, known)
	usageTotals, known := decodeFinalUsageTotals(`{"input_tokens":0,"output_tokens":0,"reported":true}`)
	require.True(t, known)

	total, err := usageTotals.TotalTokens()
	require.NoError(t, err)
	assert.Zero(t, total)

	_, known = decodeFinalUsageTotals(`{"input_tokens":-1,"reported":true}`)
	assert.False(t, known)
}

func TestRecordAgentTaskUsageTotalsRefreshesWorkflowSummaryMetric(t *testing.T) {
	t.Parallel()

	child := testAgentTask(database.TaskRunning)
	child.Task.ID = summaryMetricFirstItem
	app := newTestApp()
	app.workflowSteps = map[string][]database.WorkflowAgentTaskDetail{
		summaryWorkflowID: {{
			AgentTask: child,
			Link: database.WorkflowAgentTaskEntity{
				CreatedAt: time.Time{}, WorkflowTaskID: summaryWorkflowID, AgentTaskID: child.Task.ID,
				NodeKey: "summary-node", InvocationIndex: 0, Sequence: 0,
			},
		}},
	}
	app.refreshWorkflowSummaryMetrics()

	app.recordAgentTaskUsageTotals(
		summaryMetricFirstItem,
		`{"input_tokens":10,"output_tokens":5,"reported":true}`,
	)

	metrics := app.workflowSummaryMetrics[summaryWorkflowID]
	assert.Equal(t, 1, metrics.total)
	assert.Equal(t, "15 tok", usageTotalsSuffix(metrics.usage, metrics.total))
}

func TestRecordAgentTaskUsageTotalsRejectsRegressiveSnapshots(t *testing.T) {
	t.Parallel()

	app := newTestApp()
	app.agentTaskUsageTotals = map[string]model.UsageTotals{}
	app.recordAgentTaskUsageTotals(summaryMetricFirstItem, `{"input_tokens":10,"output_tokens":5,"reported":true}`)
	app.recordAgentTaskUsageTotals(
		summaryMetricFirstItem,
		`{"input_tokens":10,"output_tokens":5,"provider_round_trips":1,"reported":true}`,
	)
	app.recordAgentTaskUsageTotals(
		summaryMetricFirstItem,
		`{"input_tokens":9,"output_tokens":6,"provider_round_trips":2,"reported":true}`,
	)

	assert.Equal(t, model.UsageTotals{
		InputTokens: 10, OutputTokens: 5, ProviderRoundTrips: 1, Reported: true,
	}, app.agentTaskUsageTotals[summaryMetricFirstItem])
}

func TestDesiredAgentTaskWatchesIncludesWorkflowChildrenInspectedTaskAndDropsTerminalTasks(t *testing.T) {
	t.Parallel()

	independent := testAgentTask(database.TaskRunning)
	independent.Task.ID = "independent"
	child := testAgentTask(database.TaskRunning)
	child.Task.ID = summaryWorkflowChildID
	app := newTestApp()
	app.inspectedAgentTaskID = summaryInspectedTaskID
	app.agentTasks = []database.AgentTaskEntity{independent}
	app.workflowSteps = map[string][]database.WorkflowAgentTaskDetail{
		summaryWorkflowID: {{
			AgentTask: child,
			Link: database.WorkflowAgentTaskEntity{
				CreatedAt: time.Time{}, WorkflowTaskID: summaryWorkflowID, AgentTaskID: child.Task.ID,
				NodeKey: "summary-node", InvocationIndex: 0, Sequence: 1,
			},
		}},
	}

	assert.Equal(t, map[string]struct{}{
		"independent": {}, summaryInspectedTaskID: {}, summaryWorkflowChildID: {},
	}, app.desiredAgentTaskWatches())

	app.agentTasks[0].Task.State = database.TaskSucceeded
	app.workflowSteps[summaryWorkflowID][0].AgentTask.Task.State = database.TaskCanceled
	assert.Equal(t, map[string]struct{}{summaryInspectedTaskID: {}}, app.desiredAgentTaskWatches())
}

func TestHydrateFinalAgentTaskUsageTotalsClearsLiveTerminalUsage(t *testing.T) {
	t.Parallel()

	app := newTestApp()
	app.agentTaskUsageTotals = map[string]model.UsageTotals{
		summaryMetricFirstItem: {
			InputTokens: 10, OutputTokens: 5, ProviderRoundTrips: 1, Reported: true,
		},
	}
	task := testAgentTask(database.TaskFailed)
	task.Task.ID = summaryMetricFirstItem
	task.UsageJSON = `{}`

	app.hydrateFinalAgentTaskUsageTotals(&task)

	_, found := app.agentTaskUsageTotals[summaryMetricFirstItem]
	assert.False(t, found)
}
