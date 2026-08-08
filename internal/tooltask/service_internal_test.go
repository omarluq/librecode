package tooltask

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite" // Register the SQLite driver used by sql.Open.

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/taskruntime"
	"github.com/omarluq/librecode/internal/tool"
)

const testInvocationID = "inv"

func TestServiceStartRejectsNilRequest(t *testing.T) {
	t.Parallel()

	_, err := New(nil, nil, time.Minute, time.Hour, 1024).Start(t.Context(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start background tool task: request is required")
}

func TestServiceStartDeduplicatesBeforeAdmissionHook(t *testing.T) {
	t.Parallel()

	service, _, ownerID := newTestService(t, time.Minute)
	arguments, err := tool.ArgumentsFromRaw([]byte(`{"path":"go.mod"}`))
	require.NoError(t, err)

	admissions := 0
	request := func() *StartRequest {
		return &StartRequest{
			Invocation: Invocation{
				ID: "invocation-1", WrapperCallID: "wrapper-1", OwnerSessionID: ownerID, CWD: t.TempDir(),
				ParentCallID: "", InitiatingEntryID: "", SourceSequence: 0,
			},
			Target: string(tool.NameRead), Arguments: arguments, Timeout: time.Minute,
			Admit: func(_ context.Context, _ *StartRequest) error {
				admissions++

				return nil
			},
		}
	}
	created, err := service.Start(t.Context(), request())
	require.NoError(t, err)
	duplicate, err := service.Start(t.Context(), request())
	require.NoError(t, err)
	assert.Equal(t, created.Task.ID, duplicate.Task.ID)
	assert.Equal(t, 1, admissions)
}

func TestServiceStartSerializesOnlyMatchingInvocations(t *testing.T) {
	t.Parallel()

	service, _, owner := newTestService(t, time.Minute)
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	cwd := t.TempDir()
	firstArguments := mustArguments(t, `{"path":"first"}`)
	secondArguments := mustArguments(t, `{"path":"second"}`)

	go func() {
		_, err := service.Start(t.Context(), &StartRequest{
			Invocation: Invocation{
				ID: "first", WrapperCallID: "wrapper-first", ParentCallID: "", OwnerSessionID: owner, CWD: cwd,
				InitiatingEntryID: "", SourceSequence: 0,
			},
			Target: string(tool.NameRead), Arguments: firstArguments, Timeout: 0,
			Admit: func(context.Context, *StartRequest) error {
				close(firstEntered)
				<-releaseFirst

				return nil
			},
		})
		firstDone <- err
	}()

	<-firstEntered

	secondEntered := make(chan struct{})
	secondDone := make(chan error, 1)

	go func() {
		_, err := service.Start(t.Context(), &StartRequest{
			Invocation: Invocation{
				ID: "second", WrapperCallID: "wrapper-second", ParentCallID: "", OwnerSessionID: owner, CWD: cwd,
				InitiatingEntryID: "", SourceSequence: 0,
			},
			Target: string(tool.NameRead), Arguments: secondArguments, Timeout: 0,
			Admit: func(context.Context, *StartRequest) error {
				close(secondEntered)

				return nil
			},
		})
		secondDone <- err
	}()

	select {
	case <-secondEntered:
	case <-time.After(time.Second):
		t.Fatal("unrelated admission was globally serialized")
	}

	close(releaseFirst)
	require.NoError(t, <-firstDone)
	require.NoError(t, <-secondDone)
}

func TestEligibleToolsRemainExplicitSafeAllowlist(t *testing.T) {
	t.Parallel()

	for _, name := range []tool.Name{
		tool.NameRead, tool.NameGrep, tool.NameFind, tool.NameLS, tool.NameAST,
		tool.NameBash, tool.NameEdit, tool.NameWrite,
	} {
		assert.True(t, Eligible(name), name)
	}

	assert.False(t, Eligible(tool.NameFetch))
}

func TestForegroundAndBackgroundMutationsShareCoordinator(t *testing.T) {
	t.Parallel()

	if err := verifyRepositoryBash(); err != nil {
		t.Skipf("repository Bash is unavailable: %v", err)
	}

	coordinator := tool.NewCoordinator()
	cwd := t.TempDir()
	foreground, err := tool.NewRegistryWithCoordinator(cwd, []tool.Name{tool.NameBash}, coordinator)
	require.NoError(t, err)
	competingBash, err := tool.NewRegistryWithCoordinator(cwd, []tool.Name{tool.NameBash}, coordinator)
	require.NoError(t, err)
	background, err := tool.NewRegistryWithCoordinator(cwd, []tool.Name{tool.NameWrite}, coordinator)
	require.NoError(t, err)

	bashArgs, err := tool.ArgumentsFromRaw([]byte(
		`{"command":"touch foreground-started; while [ ! -f release ]; do sleep 0.01; done"}`,
	))
	require.NoError(t, err)
	bashCall, err := foreground.Prepare("bash", bashArgs)
	require.NoError(t, err)
	writeArgs, err := tool.ArgumentsFromRaw([]byte(`{"path":"background.txt","content":"done"}`))
	require.NoError(t, err)
	writeCall, err := background.Prepare("write", writeArgs)
	require.NoError(t, err)

	bashDone := make(chan error, 1)

	go func() { _, executeErr := bashCall.Execute(t.Context()); bashDone <- executeErr }()

	require.Eventually(t, func() bool {
		return fileExists(filepath.Join(cwd, "foreground-started"))
	}, 10*time.Second, 10*time.Millisecond)

	writeDone := make(chan error, 1)

	go func() { _, executeErr := writeCall.Execute(t.Context()); writeDone <- executeErr }()

	otherArgs, err := tool.ArgumentsFromRaw([]byte(`{"command":"touch competing-bash"}`))
	require.NoError(t, err)
	otherCall, err := competingBash.Prepare("bash", otherArgs)
	require.NoError(t, err)

	otherDone := make(chan error, 1)

	go func() { _, executeErr := otherCall.Execute(t.Context()); otherDone <- executeErr }()

	select {
	case err := <-writeDone:
		require.FailNow(t, "background mutation overlapped foreground Bash", "error: %v", err)
	case err := <-otherDone:
		require.FailNow(t, "Bash workspace reservations overlapped", "error: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	assert.NoFileExists(t, filepath.Join(cwd, "competing-bash"))
	require.NoError(t, touch(filepath.Join(cwd, "release")))
	require.NoError(t, <-bashDone)
	require.NoError(t, <-writeDone)
	require.NoError(t, <-otherDone)
}

func verifyRepositoryBash() error {
	registry, err := tool.NewRegistryWithTools(".", []tool.Name{tool.NameBash})
	if err != nil {
		return fmt.Errorf("create Bash registry: %w", err)
	}

	arguments, err := tool.ArgumentsFromRaw([]byte(`{"command":"true"}`))
	if err != nil {
		return fmt.Errorf("create Bash arguments: %w", err)
	}

	_, err = registry.Execute(context.Background(), string(tool.NameBash), arguments)
	if err != nil {
		return fmt.Errorf("execute Bash probe: %w", err)
	}

	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

func touch(path string) error {
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		return fmt.Errorf("touch %q: %w", path, err)
	}

	return nil
}

func newTestService(t *testing.T, defaultTimeout time.Duration) (*Service, *sql.DB, string) {
	t.Helper()

	connection, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	require.NoError(t, database.Migrate(t.Context(), connection))
	repository, err := database.NewToolTaskRepository(connection)
	require.NoError(t, err)
	sessions, err := database.NewSessionRepository(connection)
	require.NoError(t, err)
	owner, err := sessions.CreateSession(t.Context(), t.TempDir(), "owner", "")
	require.NoError(t, err)

	return New(repository, tool.NewCoordinator(), defaultTimeout, time.Hour, 4096), connection, owner.ID
}

func mustArguments(t *testing.T, raw string) tool.Arguments {
	t.Helper()

	arguments, err := tool.ArgumentsFromRaw([]byte(raw))
	require.NoError(t, err)

	return arguments
}

func startReadTask(t *testing.T, service *Service, owner, invocationID, cwd, path string) *database.ToolTaskEntity {
	t.Helper()

	entity, err := service.Start(t.Context(), &StartRequest{
		Target: string(tool.NameRead), Arguments: mustArguments(t, fmt.Sprintf(`{"path":%q}`, path)),
		Invocation: Invocation{
			ID: invocationID, WrapperCallID: "wrapper-" + invocationID, ParentCallID: "",
			OwnerSessionID: owner, CWD: cwd, InitiatingEntryID: "", SourceSequence: 0,
		},
		Timeout: 0, Admit: nil,
	})
	require.NoError(t, err)

	return entity
}

func TestValidateStartRequest(t *testing.T) {
	t.Parallel()

	const identityError = "identity is unavailable"

	valid := StartRequest{
		Target: string(tool.NameRead), Arguments: tool.EmptyArguments(), Timeout: time.Minute, Admit: nil,
		Invocation: Invocation{
			ID: testInvocationID, WrapperCallID: "call", ParentCallID: "", OwnerSessionID: "owner",
			CWD: t.TempDir(), InitiatingEntryID: "", SourceSequence: 0,
		},
	}

	tests := []struct {
		name    string
		mutate  func(*StartRequest)
		message string
	}{
		{
			name: "ineligible", mutate: func(r *StartRequest) { r.Target = string(tool.NameFetch) },
			message: "not eligible",
		},
		{
			name: "missing invocation id", mutate: func(r *StartRequest) { r.Invocation.ID = " " },
			message: identityError,
		},
		{
			name: "missing wrapper call id", mutate: func(r *StartRequest) { r.Invocation.WrapperCallID = "" },
			message: identityError,
		},
		{
			name: "missing owner", mutate: func(r *StartRequest) { r.Invocation.OwnerSessionID = "" },
			message: identityError,
		},
		{
			name: "relative cwd", mutate: func(r *StartRequest) { r.Invocation.CWD = "." },
			message: "must be absolute",
		},
		{
			name: "excessive timeout", mutate: func(r *StartRequest) { r.Timeout = 2 * time.Hour },
			message: "exceeds maximum",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := valid
			test.mutate(&request)
			err := validateStartRequest(&request, tool.Name(request.Target), time.Hour)
			require.ErrorContains(t, err, test.message)
		})
	}

	assert.NoError(t, validateStartRequest(&valid, tool.NameRead, time.Hour))
}

func TestStartAdmissionValidationAndPersistence(t *testing.T) {
	t.Parallel()

	service, _, owner := newTestService(t, 1500*time.Millisecond)
	args, err := tool.ArgumentsFromRaw([]byte(`{"path":"fixture.txt"}`))
	require.NoError(t, err)
	base := StartRequest{
		Target: string(tool.NameRead), Arguments: args, Timeout: 0, Admit: nil,
		Invocation: Invocation{ID: testInvocationID, WrapperCallID: "wrapper", ParentCallID: "parent",
			OwnerSessionID: owner, CWD: t.TempDir(), InitiatingEntryID: "entry", SourceSequence: 7},
	}

	{
		request := base
		request.Invocation.ID = "rejected"
		request.Admit = func(context.Context, *StartRequest) error { return assert.AnError }
		_, startErr := service.Start(t.Context(), &request)
		require.ErrorContains(t, startErr, "admit background tool task")
	}
	{
		request := base
		request.Invocation.ID = "mutated"
		request.Admit = func(_ context.Context, admitted *StartRequest) error {
			admitted.Target = string(tool.NameFetch)

			return nil
		}
		_, startErr := service.Start(t.Context(), &request)
		require.ErrorContains(t, startErr, "validate admitted background tool task")
	}
	{
		request := base
		request.Invocation.ID = "created"
		entity, startErr := service.Start(t.Context(), &request)
		require.NoError(t, startErr)
		assert.Equal(t, 2, entity.TimeoutSeconds)
		assert.Equal(t, "parent", entity.ParentCallID)
		assert.Equal(t, 7, entity.SourceSequence)
		assert.JSONEq(t, `{"eligible":true,"version":1}`, entity.PolicyJSON)
		assert.NotEmpty(t, entity.DefinitionJSON)
	}
}

func TestServiceLifecycleQueriesAndWait(t *testing.T) {
	t.Parallel()

	service, _, owner := newTestService(t, time.Minute)
	entity := startReadTask(t, service, owner, "wait-task", t.TempDir(), "missing.txt")

	listed, err := service.List(t.Context(), owner, []database.TaskState{database.TaskQueued}, 10)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, entity.Task.ID, listed[0].Task.ID)

	got, found, err := service.Get(t.Context(), owner, entity.Task.ID)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, entity.Task.ID, got.Task.ID)
	_, found, err = service.Get(t.Context(), entity.Task.ID, entity.Task.ID)
	require.NoError(t, err)
	assert.False(t, found)

	ctx, cancel := context.WithCancel(t.Context())
	waitDone := make(chan error, 1)

	go func() {
		_, waitErr := service.Wait(ctx, owner, entity.Task.ID)
		waitDone <- waitErr
	}()

	require.Eventually(t, func() bool {
		service.waitMu.Lock()
		defer service.waitMu.Unlock()

		return len(service.waiters[entity.Task.ID]) == 1
	}, time.Second, time.Millisecond)
	cancel()
	require.ErrorIs(t, <-waitDone, context.Canceled)
	assert.Empty(t, service.waiters)

	canceled, found, err := service.Cancel(t.Context(), owner, entity.Task.ID)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, database.TaskCanceled, canceled.Task.State)
	terminal, err := service.Wait(t.Context(), owner, entity.Task.ID)
	require.NoError(t, err)
	assert.Equal(t, database.TaskCanceled, terminal.Task.State)

	_, found, err = service.Cancel(t.Context(), entity.Task.ID, entity.Task.ID)
	require.NoError(t, err)
	assert.False(t, found)
	_, err = service.Wait(t.Context(), owner, owner)
	require.ErrorContains(t, err, "background task disappeared")
}

func TestRunExecutesPersistedCallAndCompletionHook(t *testing.T) {
	t.Parallel()

	service, _, owner := newTestService(t, time.Minute)
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "fixture.txt"), []byte("original"), 0o600))
	entity := startReadTask(t, service, owner, "run-task", cwd, "fixture.txt")

	service.SetCompletionHook(func(_ context.Context, completion *Completion) error {
		assert.Equal(t, entity.Task.ID, completion.TaskID)
		assert.Equal(t, "run-task", completion.InvocationID)
		assert.Equal(t, string(tool.NameRead), completion.Target)
		completion.Result = tool.TextResult("hooked", map[string]any{"hook": true})

		return nil
	})
	outcome, err := service.Run(t.Context(), &entity.Task, nil)
	require.NoError(t, err)
	assert.Equal(t, "hooked", outcome.Summary)
	value, ok := outcome.Value.(map[string]any)
	require.True(t, ok)
	isError, ok := value[outcomeIsErrorKey].(bool)
	require.True(t, ok)
	assert.False(t, isError)

	missing := entity.Task
	missing.ID = owner
	_, err = service.Run(t.Context(), &missing, nil)
	require.ErrorContains(t, err, "tool task not found")
}

func TestSettlePublishesCanonicalCompletionOnce(t *testing.T) {
	t.Parallel()

	service, _, owner := newTestService(t, 0)
	created := startReadTask(t, service, owner, "completion-invocation", t.TempDir(), "input.txt")
	result := tool.TextResult("done", nil)
	service.rememberCompletion(created.Task.ID, created, result, nil)
	events, cancel := service.SubscribeCompletions()

	defer cancel()

	changed, err := service.Settle(t.Context(), &database.TaskFinish{
		TaskID: created.Task.ID, From: []database.TaskState{database.TaskQueued},
		TargetState: database.TaskSucceeded, EventKind: "task_succeeded", Result: "done",
		ErrorCode: "", ErrorMessage: "", PayloadJSON: `{}`, LeaseOwner: "",
	}, taskruntime.Outcome{
		Value: outcomeValue(result, nil), Summary: "done", ErrorCode: "", ErrorMessage: "",
	})
	require.NoError(t, err)
	require.True(t, changed)

	select {
	case completion := <-events:
		assert.Equal(t, created.Task.ID, completion.TaskID)
		assert.Equal(t, created.OwnerSessionID, completion.OwnerSessionID)
		assert.Equal(t, created.TargetName, completion.Target)
		assert.JSONEq(t, created.ArgumentsJSON, completion.ArgumentsJSON)
		assert.Equal(t, "done", completion.Result.Text())
	case <-time.After(time.Second):
		t.Fatal("completion was not published")
	}

	service.publishCompletion(created.Task.ID)

	select {
	case duplicate := <-events:
		t.Fatalf("unexpected duplicate completion: %+v", duplicate)
	default:
	}
}

func TestRunRecordsCompletionHookFailureWithoutReplacingOutcome(t *testing.T) {
	t.Parallel()

	service, _, owner := newTestService(t, time.Minute)
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "fixture.txt"), []byte("original"), 0o600))
	entity := startReadTask(t, service, owner, "hook-failure", cwd, "fixture.txt")
	service.SetCompletionHook(func(context.Context, *Completion) error { return errors.New("hook failed") })

	outcome, err := service.Run(t.Context(), &entity.Task, nil)
	require.NoError(t, err)
	assert.Equal(t, "original", outcome.Summary)

	events, err := service.repository.Tasks().ListEvents(t.Context(), entity.Task.ID, 0, 10)
	require.NoError(t, err)
	require.Len(t, events, 2)
	assert.Equal(t, "tool_completion_hook_failed", events[1].Event.Kind)
	assert.JSONEq(t, `{"error":"hook failed"}`, events[1].Event.PayloadJSON)
}

func TestTryAdmitRunAndDefinitionDrift(t *testing.T) {
	t.Parallel()

	service, connection, owner := newTestService(t, time.Minute)
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "fixture.txt"), []byte("content"), 0o600))
	entity := startReadTask(t, service, owner, "admit-task", cwd, "fixture.txt")

	admitted, err := service.TryAdmit(t.Context(), &entity.Task)
	require.NoError(t, err)
	assert.True(t, admitted)
	assert.Contains(t, service.admissions, entity.Task.ID)
	outcome, err := service.Run(t.Context(), &entity.Task, nil)
	require.NoError(t, err)
	assert.Equal(t, "content", outcome.Summary)
	assert.NotContains(t, service.admissions, entity.Task.ID)

	drift := startReadTask(t, service, owner, "drift-task", cwd, "fixture.txt")
	_, err = connection.ExecContext(
		t.Context(), `UPDATE tool_tasks SET definition_json = '{}' WHERE task_id = ?`, drift.Task.ID,
	)
	require.NoError(t, err)
	admitted, err = service.TryAdmit(t.Context(), &drift.Task)
	assert.True(t, admitted)
	require.ErrorContains(t, err, "definition drift")
	outcome, err = service.Run(t.Context(), &drift.Task, nil)
	require.Error(t, err)
	assert.Equal(t, "definition_drift", outcome.ErrorCode)

	service.ReleaseAdmission("unknown")
}

func TestCompletionAndOutcomeHelpers(t *testing.T) {
	t.Parallel()

	result := tool.TextResult("original", nil)
	runErr := errors.New("run failed")
	service := New(nil, nil, time.Minute, time.Hour, 100)
	persisted := new(database.ToolTaskEntity)
	persisted.InvocationID = "inv"
	persisted.ParentCallID = "parent"
	persisted.TargetName = "read"
	persisted.ArgumentsJSON = `{}`
	persisted.SourceSequence = 3

	gotResult, gotErr := service.applyCompletionHook(t.Context(), "task", persisted, result, runErr)
	assert.Equal(t, result, gotResult)
	require.ErrorIs(t, gotErr, runErr)
	service.SetCompletionHook(func(context.Context, *Completion) error { return assert.AnError })
	gotResult, gotErr = service.applyCompletionHook(t.Context(), "task", persisted, result, runErr)
	assert.Equal(t, result, gotResult)
	require.ErrorIs(t, gotErr, runErr)

	value := outcomeValue(result, runErr)
	assert.Equal(t, "run failed", value[outcomeErrorKey])
	isError, ok := value[outcomeIsErrorKey].(bool)
	require.True(t, ok)
	assert.True(t, isError)

	unchanged, summary := boundOutcome(value, "short", 10_000)
	assert.Equal(t, value, unchanged)
	assert.Equal(t, "short", summary)
	unchanged, summary = boundOutcome(value, "short", 0)
	assert.Equal(t, value, unchanged)
	assert.Equal(t, "short", summary)
	bounded, summary := boundOutcome(value, "large", 1)
	assert.Empty(t, summary)
	assert.Equal(t, "outcome exceeded persistence limit", bounded[outcomeErrorKey])

	unicodeValue := outcomeValue(tool.TextResult(strings.Repeat("界", 100), nil), nil)
	bounded, summary = boundOutcome(unicodeValue, strings.Repeat("界", 100), 256)
	assert.True(t, utf8.ValidString(summary))

	encoded, err := json.Marshal(bounded)
	require.NoError(t, err)
	assert.True(t, utf8.Valid(encoded))
}

func TestStateAndEntityHelpers(t *testing.T) {
	t.Parallel()

	for _, state := range []database.TaskState{
		database.TaskSucceeded, database.TaskFailed, database.TaskCanceled, database.TaskInterrupted,
	} {
		assert.True(t, isTerminal(state), state)
	}

	for _, state := range []database.TaskState{
		database.TaskQueued, database.TaskRunning, database.TaskCanceling, database.TaskState("unknown"),
	} {
		assert.False(t, isTerminal(state), state)
	}

	assert.Equal(t, database.TaskKindTool, New(nil, nil, 0, 0, 0).Kind())

	request := new(StartRequest)
	request.Target = "read"
	request.Arguments = mustArguments(t, `{"path":"x"}`)
	request.Invocation = Invocation{
		ID: "inv", WrapperCallID: "wrapper", ParentCallID: "", OwnerSessionID: "owner", CWD: "/tmp",
		InitiatingEntryID: "", SourceSequence: 0,
	}
	entity := newToolTaskEntity(request, time.Millisecond, `{}`)
	assert.Equal(t, 1, entity.TimeoutSeconds)
	assert.JSONEq(t, `{"path":"x"}`, entity.ArgumentsJSON)
}

func TestRepositoryErrorsAreWrapped(t *testing.T) {
	t.Parallel()

	service, _, _ := newTestService(t, time.Minute)
	service.AttachRuntime(nil)
	_, _, err := service.Get(t.Context(), "invalid", "invalid")
	require.ErrorContains(t, err, "get background tool task")
	_, err = service.List(t.Context(), "invalid", nil, 10)
	require.ErrorContains(t, err, "list background tool tasks")
	_, _, err = service.Cancel(t.Context(), "invalid", "invalid")
	require.ErrorContains(t, err, "cancel background tool task")

	changed, err := service.Settle(t.Context(), new(database.TaskFinish), taskruntime.Outcome{
		Value: map[string]any{}, Summary: "", ErrorCode: "", ErrorMessage: "",
	})
	assert.False(t, changed)
	require.ErrorContains(t, err, "settle background tool task")

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorContains(t, service.RecoverExpired(ctx, time.Now()), "recover background tool tasks")
}

func TestRecoverExpiredReleasesRecoveredAdmission(t *testing.T) {
	t.Parallel()

	service, _, owner := newTestService(t, time.Minute)
	entity, err := service.Start(t.Context(), &StartRequest{
		Target: string(tool.NameWrite), Arguments: mustArguments(t, `{"path":"fixture.txt","content":"content"}`),
		Invocation: Invocation{ID: "recover-admit", WrapperCallID: "wrapper-recover-admit", ParentCallID: "",
			OwnerSessionID: owner, CWD: t.TempDir(), InitiatingEntryID: "", SourceSequence: 0},
		Timeout: 0, Admit: nil,
	})
	require.NoError(t, err)
	admitted, err := service.TryAdmit(t.Context(), &entity.Task)
	require.NoError(t, err)
	require.True(t, admitted)

	reserved := service.admissions[entity.Task.ID]

	claimed, err := service.repository.Tasks().ClaimQueued(t.Context(), &database.TaskClaim{
		TaskID: entity.Task.ID, LeaseOwner: "expired-worker", LeaseExpiresAt: time.Now().Add(-time.Minute),
		EventKind: "task_started",
	})
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, service.RecoverExpired(t.Context(), time.Now()))
	assert.NotContains(t, service.admissions, entity.Task.ID)
	assert.True(t, reserved.TryAdmit(), "recovered task reservation was not released")
	reserved.Release()
}

func TestSettleRejectsUnencodableOutcomeAndRecoverExpired(t *testing.T) {
	t.Parallel()

	service, _, _ := newTestService(t, time.Minute)
	changed, err := service.Settle(t.Context(), new(database.TaskFinish), taskruntime.Outcome{
		Value: map[string]any{"invalid": func() {}}, Summary: "", ErrorCode: "", ErrorMessage: "",
	})
	assert.False(t, changed)
	require.ErrorContains(t, err, "encode tool task outcome")
	assert.NoError(t, service.RecoverExpired(t.Context(), time.Now()))
}

func TestBoundOutcomeEnforcesHardEncodedLimit(t *testing.T) {
	t.Parallel()

	const maximum = 256

	value := map[string]any{
		outcomeResultKey: tool.TextResult(
			strings.Repeat("x", 4096), map[string]any{"large": strings.Repeat("y", 4096)},
		),
		outcomeErrorKey: "", outcomeIsErrorKey: false, outcomeTruncatedKey: false,
	}

	bounded, summary := boundOutcome(value, strings.Repeat("x", 4096), maximum)
	encoded, err := json.Marshal(bounded)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(encoded), maximum)
	assert.Less(t, len(summary), 4096)
	assert.Equal(t, true, bounded[outcomeTruncatedKey])
}
