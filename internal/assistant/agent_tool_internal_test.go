package assistant

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite" // Register the SQLite database/sql driver used by these tests.

	"github.com/omarluq/librecode/internal/agent"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/tool"
)

type agentControllerStub struct {
	submitErr     error
	getErr        error
	cancelErr     error
	listErr       error
	getFunc       func(int) (*database.AgentTaskEntity, bool, error)
	submitFunc    func() (*database.AgentTaskEntity, error)
	cancelFound   *bool
	lastSubmit    *AgentTaskRequest
	task          *database.AgentTaskEntity
	listed        []database.AgentTaskEntity
	lastLimit     int
	getCalls      int
	subscriptions int
	found         bool
}

func (stub *agentControllerStub) SubmitAgentTask(
	_ context.Context,
	request *AgentTaskRequest,
) (*database.AgentTaskEntity, error) {
	stub.lastSubmit = request
	if stub.submitFunc != nil {
		return stub.submitFunc()
	}

	return stub.task, stub.submitErr
}
func (stub *agentControllerStub) Get(context.Context, string) (*database.AgentTaskEntity, bool, error) {
	stub.getCalls++
	if stub.getFunc != nil {
		return stub.getFunc(stub.getCalls)
	}

	return stub.task, stub.found, stub.getErr
}
func (stub *agentControllerStub) List(_ context.Context, _ string, limit int) ([]database.AgentTaskEntity, error) {
	stub.lastLimit = limit

	return stub.listed, stub.listErr
}
func (stub *agentControllerStub) Cancel(context.Context, string, string) (*database.TaskEntity, bool, error) {
	found := stub.found
	if stub.cancelFound != nil {
		found = *stub.cancelFound
	}

	if stub.task == nil {
		return nil, found, stub.cancelErr
	}

	return &stub.task.Task, found, stub.cancelErr
}
func (stub *agentControllerStub) Await(context.Context, string) (*database.AgentTaskEntity, error) {
	return stub.task, nil
}
func (stub *agentControllerStub) SubscribeAgentTask(
	string,
) (events <-chan database.TaskEventEntity, cancel func()) {
	stub.subscriptions++
	channel := make(chan database.TaskEventEntity)
	close(channel)

	return channel, func() {}
}

func TestAgentToolDefinitionsAndDispatch(t *testing.T) {
	t.Parallel()

	catalog := isolatedAgentCatalog(t)
	for _, name := range []tool.Name{
		agentStartToolName, agentStatusToolName, agentWaitToolName, agentCancelToolName, agentListToolName,
	} {
		executor := newAgentToolExecutor(nil, nil, catalog, name, "", "")
		definition := executor.Definition()
		assert.Equal(t, name, definition.Name)
		assert.NotEmpty(t, definition.Schema)
	}

	executor := newAgentToolExecutor(nil, nil, catalog, tool.Name("unknown"), "", "")
	_, err := executor.Execute(t.Context(), tool.EmptyArguments())
	require.ErrorContains(t, err, "unknown agent tool")
}

func TestAgentStartCreatesChildAndSubmitsSnapshot(t *testing.T) {
	t.Parallel()
	sessions := agentToolSessions(t)
	parent, err := sessions.CreateSession(t.Context(), t.TempDir(), "parent", "")
	require.NoError(t, err)

	stub := newAgentControllerStub(agentToolTask("task-1", parent.ID, database.TaskQueued), nil, true)
	executor := newAgentToolExecutor(
		stub, sessions, isolatedAgentCatalog(t), agentStartToolName, parent.ID, parent.CWD,
	)

	result, err := executor.Execute(t.Context(), agentArguments(t, `{"agent":" GENERAL ","prompt":"  inspect code  "}`))
	require.NoError(t, err)
	assert.Contains(t, result.Text(), "Started general agent task task-1")
	require.NotNil(t, stub.lastSubmit)
	assert.Equal(t, "inspect code", stub.lastSubmit.Prompt)
	assert.Equal(t, parent.ID, stub.lastSubmit.OwnerSessionID)
	assert.Contains(t, stub.lastSubmit.PolicyJSON, `"name":"general"`)
	child, found, getErr := sessions.GetSession(t.Context(), stub.lastSubmit.ChildSessionID)
	require.NoError(t, getErr)
	require.True(t, found)
	assert.Equal(t, parent.ID, child.ParentSession)
}

func TestAgentStartValidationAndSubmissionCleanup(t *testing.T) {
	t.Parallel()
	catalog := isolatedAgentCatalog(t)

	tests := []struct{ name, raw, want string }{
		{name: "decode", raw: `{"agent":1,"prompt":"work"}`, want: "decode tool input"},
		{name: "missing", raw: `{}`, want: "required"},
		{name: "blank", raw: `{"agent":"general","prompt":"  "}`, want: "required"},
		{name: "unknown", raw: `{"agent":"missing","prompt":"work"}`, want: "unknown agent"},
		{
			name: "too long",
			raw:  `{"agent":"general","prompt":"` + strings.Repeat("x", maxAgentPromptBytes+1) + `"}`,
			want: "at most",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			executor := newAgentToolExecutor(new(agentControllerStub), nil, catalog, agentStartToolName, "p", "")

			_, err := executor.Execute(t.Context(), agentArguments(t, testCase.raw))
			if testCase.want == "" {
				assert.NoError(t, err)
			} else {
				require.ErrorContains(t, err, testCase.want)
			}
		})
	}

	sessions := agentToolSessions(t)
	parent, err := sessions.CreateSession(t.Context(), t.TempDir(), "parent", "")
	require.NoError(t, err)

	stub := new(agentControllerStub)
	stub.submitErr = errors.New("reject")
	executor := newAgentToolExecutor(stub, sessions, catalog, agentStartToolName, parent.ID, parent.CWD)
	_, err = executor.Execute(t.Context(), agentArguments(t, `{"agent":"general","prompt":"work"}`))
	require.ErrorContains(t, err, "submit agent task")
	_, found, getErr := sessions.GetSession(t.Context(), stub.lastSubmit.ChildSessionID)
	require.NoError(t, getErr)
	assert.False(t, found)
}

func TestAgentTaskOperationsAreOwnerScoped(t *testing.T) {
	t.Parallel()

	stub := newAgentControllerStub(
		agentToolTask("task-1", "owner", database.TaskRunning),
		[]database.AgentTaskEntity{{
			Task:           agentToolTaskEntity("task-1", "owner", database.TaskRunning),
			ChildSessionID: "", AgentName: "", Prompt: "", Model: "", Provider: "",
			PolicyJSON: "", UsageJSON: "", Depth: 0,
		}},
		true,
	)
	catalog := isolatedAgentCatalog(t)

	for _, name := range []tool.Name{agentStatusToolName, agentWaitToolName, agentCancelToolName} {
		executor := newAgentToolExecutor(stub, nil, catalog, name, "owner", "")
		result, err := executor.Execute(t.Context(), agentArguments(t, `{"task_id":" task-1 "}`))
		require.NoError(t, err)
		assert.Contains(t, result.Text(), "task-1 is running")
	}

	executor := newAgentToolExecutor(stub, nil, catalog, agentListToolName, "owner", "")
	result, err := executor.Execute(t.Context(), agentArguments(t, `{"limit":999}`))
	require.NoError(t, err)
	assert.Equal(t, maxAgentListLimit, stub.lastLimit)
	assert.Equal(t, "task-1 running", result.Text())

	stub.task.Task.OwnerSessionID = "other"
	executor.name = agentStatusToolName
	_, err = executor.Execute(t.Context(), agentArguments(t, `{"task_id":"task-1"}`))
	require.ErrorContains(t, err, "not found")
	_, err = executor.Execute(t.Context(), agentArguments(t, `{"task_id":" "}`))
	require.ErrorContains(t, err, "required")
}

func TestAgentToolDecodeErrors(t *testing.T) {
	t.Parallel()
	catalog := isolatedAgentCatalog(t)

	const invalidTaskID = `{"task_id":1}`

	decodeInputs := map[tool.Name]string{
		agentStartToolName:  `{"agent":1}`,
		agentStatusToolName: invalidTaskID,
		agentWaitToolName:   invalidTaskID,
		agentCancelToolName: invalidTaskID,
		agentListToolName:   `{"limit":"many"}`,
	}
	for name, raw := range decodeInputs {
		executor := newAgentToolExecutor(new(agentControllerStub), nil, catalog, name, "owner", "")
		_, err := executor.Execute(t.Context(), agentArguments(t, raw))
		require.ErrorContains(t, err, "decode tool input")
	}
}

func TestAgentToolErrorAndResultBranches(t *testing.T) {
	t.Parallel()
	catalog := isolatedAgentCatalog(t)
	stub := newAgentControllerStub(agentToolTask("task", "owner", database.TaskRunning), nil, true)
	executor := newAgentToolExecutor(stub, nil, catalog, "", "owner", "")

	executor.name = agentWaitToolName
	stub.getErr = errors.New("get failed")
	result, err := executor.Execute(t.Context(), agentArguments(t, `{"task_id":" task "}`))
	require.ErrorContains(t, err, "get agent task")
	assert.Equal(t, "task", result.Details["task_id"])

	executor.name = agentCancelToolName
	stub.getErr = nil
	stub.found = false
	_, err = executor.Execute(t.Context(), agentArguments(t, `{"task_id":"task"}`))
	require.ErrorContains(t, err, "not found")

	stub.found = true
	stub.cancelErr = errors.New("cancel failed")
	_, err = executor.Execute(t.Context(), agentArguments(t, `{"task_id":"task"}`))
	require.ErrorContains(t, err, "cancel failed")

	stub.cancelErr = nil
	cancelFound := false
	stub.cancelFound = &cancelFound
	_, err = executor.Execute(t.Context(), agentArguments(t, `{"task_id":"task"}`))
	require.ErrorContains(t, err, "not found")

	stub.cancelFound = nil
	stub.getFunc = func(call int) (*database.AgentTaskEntity, bool, error) {
		if call == stub.getCalls && call%2 == 0 {
			return nil, false, nil
		}

		return stub.task, true, nil
	}
	stub.getCalls = 0
	_, err = executor.Execute(t.Context(), agentArguments(t, `{"task_id":"task"}`))
	require.ErrorContains(t, err, "not found")

	stub.getFunc = func(call int) (*database.AgentTaskEntity, bool, error) {
		if call%2 == 0 {
			return nil, false, errors.New("refresh failed")
		}

		return stub.task, true, nil
	}
	stub.getCalls = 0
	_, err = executor.Execute(t.Context(), agentArguments(t, `{"task_id":"task"}`))
	require.ErrorContains(t, err, "refresh failed")
}

func TestAgentListAndResultBranches(t *testing.T) {
	t.Parallel()
	catalog := isolatedAgentCatalog(t)
	stub := newAgentControllerStub(nil, nil, false)
	stub.listErr = errors.New("list failed")
	executor := newAgentToolExecutor(stub, nil, catalog, agentListToolName, "owner", "")

	_, err := executor.Execute(t.Context(), agentArguments(t, `{}`))
	require.ErrorContains(t, err, "list failed")
	assert.Equal(t, defaultAgentListLimit, stub.lastLimit)
	stub.listErr = nil
	result, err := executor.Execute(t.Context(), agentArguments(t, `{"limit":-2}`))
	require.NoError(t, err)
	assert.Empty(t, result.Text())
	assert.Equal(t, defaultAgentListLimit, stub.lastLimit)

	task := agentToolTask("done", "owner", database.TaskFailed)
	task.Task.Result = "partial result"
	task.Task.ErrorMessage = "boom"
	result = agentTaskResult(task)
	assert.Equal(t, "partial result\nboom", result.Text())
	assert.Equal(t, "done", result.Details["task_id"])

	assert.Panics(t, func() { mustToolSchema(`not json`) })
}

func TestAgentSubmitFailureBranches(t *testing.T) {
	t.Parallel()
	catalog := isolatedAgentCatalog(t)

	sessions := agentToolSessions(t)
	parent, err := sessions.CreateSession(t.Context(), t.TempDir(), "parent", "")
	require.NoError(t, err)

	stub := newAgentControllerStub(agentToolTask("accepted", parent.ID, database.TaskQueued), nil, false)
	stub.submitErr = errors.New("persist response lost")
	executor := newAgentToolExecutor(stub, sessions, catalog, agentStartToolName, parent.ID, parent.CWD)
	result, err := executor.Execute(t.Context(), agentArguments(t, `{"agent":"general","prompt":"work"}`))
	require.ErrorContains(t, err, "submit agent task")
	assert.Empty(t, result.Text())
	assert.Equal(t, "accepted", result.Details["task_id"])

	closedSessions, databaseConnection := agentToolSessionsWithDB(t)
	parent, err = closedSessions.CreateSession(t.Context(), t.TempDir(), "parent", "")
	require.NoError(t, err)

	stub = new(agentControllerStub)
	stub.submitErr = errors.New("submit failed")
	stub.submitFunc = func() (*database.AgentTaskEntity, error) {
		require.NoError(t, databaseConnection.Close())

		return nil, stub.submitErr
	}
	executor = newAgentToolExecutor(stub, closedSessions, catalog, agentStartToolName, parent.ID, parent.CWD)
	_, err = executor.Execute(t.Context(), agentArguments(t, `{"agent":"general","prompt":"work"}`))
	require.ErrorContains(t, err, "submit failed")
	require.ErrorContains(t, err, "database is closed")

	closedSessions, databaseConnection = agentToolSessionsWithDB(t)
	require.NoError(t, databaseConnection.Close())

	executor.sessions = closedSessions
	_, err = executor.Execute(t.Context(), agentArguments(t, `{"agent":"general","prompt":"work"}`))
	require.ErrorContains(t, err, "create child agent session")
}

func TestChildSessionNameNormalizesAndTruncates(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "general: a b", childSessionName("general", " a\n b "))
	name := childSessionName("agent", strings.Repeat("界", 100))
	assert.Len(t, []rune(name), maxChildSessionNameRunes)
	assert.True(t, strings.HasSuffix(name, "…"))
}

func isolatedAgentCatalog(t *testing.T) *agent.Catalog {
	t.Helper()

	return agent.Load(t.TempDir())
}

func agentArguments(t *testing.T, raw string) tool.Arguments {
	t.Helper()

	arguments, err := tool.ArgumentsFromRaw([]byte(raw))
	require.NoError(t, err)

	return arguments
}

func agentToolSessions(t *testing.T) *database.SessionRepository {
	t.Helper()
	repository, _ := agentToolSessionsWithDB(t)

	return repository
}

func agentToolSessionsWithDB(t *testing.T) (*database.SessionRepository, *sql.DB) {
	t.Helper()
	databaseName := strings.ReplaceAll(t.Name(), "/", "_") + time.Now().Format("150405.000000000")
	databaseConnection, err := sql.Open("sqlite", "file:"+databaseName+"?mode=memory&cache=shared")
	require.NoError(t, err)
	databaseConnection.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := databaseConnection.Close(); err != nil && !strings.Contains(err.Error(), "database is closed") {
			require.NoError(t, err)
		}
	})
	require.NoError(t, database.Migrate(t.Context(), databaseConnection))

	return database.NewSessionRepository(databaseConnection), databaseConnection
}

func newAgentControllerStub(
	task *database.AgentTaskEntity,
	listed []database.AgentTaskEntity,
	found bool,
) *agentControllerStub {
	stub := new(agentControllerStub)
	stub.task = task
	stub.listed = listed
	stub.found = found

	return stub
}

func newAgentToolExecutor(
	controller AgentTaskController,
	sessions *database.SessionRepository,
	catalog *agent.Catalog,
	name tool.Name,
	parentSessionID string,
	cwd string,
) *agentToolExecutor {
	executor := new(agentToolExecutor)
	executor.controller = controller
	executor.sessions = sessions
	executor.catalog = catalog
	executor.name = name
	executor.parentSessionID = parentSessionID
	executor.cwd = cwd

	return executor
}

func agentToolTaskEntity(id, owner string, state database.TaskState) database.TaskEntity {
	return database.TaskEntity{
		CreatedAt: time.Time{}, StartedAt: nil, FinishedAt: nil, UpdatedAt: time.Time{}, LeaseExpiresAt: nil,
		ID: id, Kind: database.TaskKindAgent, ParentTaskID: "", OwnerSessionID: owner,
		ConcurrencyKey: owner, LeaseOwner: "", State: state, Result: "", ErrorCode: "", ErrorMessage: "",
	}
}

func agentToolTask(id, owner string, state database.TaskState) *database.AgentTaskEntity {
	return &database.AgentTaskEntity{
		Task: agentToolTaskEntity(id, owner, state), ChildSessionID: "child", AgentName: "general", Prompt: "work",
		Model: "", Provider: "", PolicyJSON: `{}`, UsageJSON: `{}`, Depth: 1,
	}
}
