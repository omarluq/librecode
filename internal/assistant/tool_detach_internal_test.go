package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/agent"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/tool"
	"github.com/omarluq/librecode/internal/tooltask"
)

type detachableTaskController struct {
	started chan struct{}
	done    chan struct{}
	entity  *database.ToolTaskEntity
	once    sync.Once
	mu      sync.Mutex
}

func newDetachableTaskController(t *testing.T, result tool.Result) *detachableTaskController {
	t.Helper()

	outcome, err := json.Marshal(map[string]any{"result": result, "error": "", "is_error": false})
	require.NoError(t, err)

	encoded := string(outcome)

	return &detachableTaskController{
		started: make(chan struct{}), done: make(chan struct{}), once: sync.Once{}, mu: sync.Mutex{},
		entity: &database.ToolTaskEntity{
			OutcomeVersion: nil, OutcomeJSON: &encoded,
			Task: database.TaskEntity{
				CreatedAt: time.Time{}, StartedAt: nil, FinishedAt: nil, UpdatedAt: time.Time{}, LeaseExpiresAt: nil,
				ID: uuid.Must(uuid.NewV7()).String(), Kind: database.TaskKindTool, ParentTaskID: "",
				OwnerSessionID: "", ConcurrencyKey: "", LeaseOwner: "", State: database.TaskRunning,
				Result: "", ErrorCode: "", ErrorMessage: "",
			},
			WrapperCallID: "", OwnerSessionID: "", InvocationID: "", CWD: "", ParentCallID: "",
			InitiatingEntryID: "", PolicyJSON: "", DefinitionJSON: "", ArgumentsJSON: "",
			TargetName:     string(tool.NameRead),
			SourceSequence: 0, TimeoutSeconds: 0,
		},
	}
}

func (controller *detachableTaskController) Start(
	context.Context,
	*tooltask.StartRequest,
) (*database.ToolTaskEntity, error) {
	controller.once.Do(func() { close(controller.started) })

	return controller.snapshot(), nil
}
func (controller *detachableTaskController) Wait(ctx context.Context, _, _ string) (*database.ToolTaskEntity, error) {
	select {
	case <-controller.done:
		controller.mu.Lock()
		controller.entity.Task.State = database.TaskSucceeded
		controller.mu.Unlock()

		return controller.snapshot(), nil
	case <-ctx.Done():
		return nil, errors.Join(errors.New("wait for detachable task"), ctx.Err())
	}
}
func (controller *detachableTaskController) Get(
	context.Context,
	string,
	string,
) (*database.ToolTaskEntity, bool, error) {
	return controller.snapshot(), true, nil
}
func (*detachableTaskController) List(
	context.Context,
	string,
	[]database.TaskState,
	int,
) ([]database.ToolTaskEntity, error) {
	return nil, nil
}
func (controller *detachableTaskController) Cancel(
	context.Context,
	string,
	string,
) (*database.ToolTaskEntity, bool, error) {
	return controller.snapshot(), true, nil
}

func (controller *detachableTaskController) snapshot() *database.ToolTaskEntity {
	controller.mu.Lock()
	defer controller.mu.Unlock()

	snapshot := *controller.entity
	snapshot.Task = controller.entity.Task

	return &snapshot
}

func TestExecutionProfileViewsShareAttachmentSynchronization(t *testing.T) {
	t.Parallel()

	runtime := NewRuntime(&RuntimeOptions{
		Config: nil, Sessions: nil, Extensions: nil, Cache: nil, Models: nil, Client: nil, Logger: nil,
		SkillsCache: nil, Agents: nil, AgentTasks: nil, WorkflowSubmitter: nil,
		ToolTasks: nil, ToolCoordinator: nil,
	})
	profile := &ExecutionProfile{
		Kind: ExecutionAgentTask, AgentName: defaultWorkflowAgentName, SystemPrompt: "", Provider: "", Model: "",
		ThinkingLevel: "", PermissionMode: agent.PermissionDeny, Tools: nil,
		EnableSkills: false, EnableExtensions: false, MaxTurns: 1, Depth: 1,
	}
	first := runtime.WithExecutionProfile(profile)
	second := runtime.WithExecutionProfile(profile)
	require.Same(t, runtime.attachments, first.attachments)
	require.Same(t, first.attachments, second.attachments)

	const attachmentCount = 50

	var waitGroup sync.WaitGroup
	waitGroup.Add(attachmentCount)

	for index := range attachmentCount {
		go func() {
			defer waitGroup.Done()

			callID := fmt.Sprintf("profile-call-%d", index)
			taskID := fmt.Sprintf("profile-task-%d", index)
			attachment := &foregroundAttachment{
				err: nil, resolved: make(chan struct{}), entity: nil, taskID: taskID, state: attachmentUnresolved,
			}

			first.attachments.mu.Lock()
			first.attachments.items[callID] = attachment
			first.attachments.mu.Unlock()

			actualTaskID, detached := second.DetachForegroundTool(callID)
			assert.True(t, detached)
			assert.Equal(t, taskID, actualTaskID)

			first.attachments.mu.Lock()
			delete(first.attachments.items, callID)
			first.attachments.mu.Unlock()
		}()
	}

	waitGroup.Wait()
}

func TestForegroundToolCompletionWinsWithoutHandle(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(directory, "input.txt"), []byte("ordinary result"), 0o600))
	controller := newDetachableTaskController(t, tool.TextResult("ordinary result", nil))
	runtime := newDetachTestRuntime(controller)
	registry := tool.NewRegistry(directory)

	done := make(chan []ToolEvent, 1)
	errs := make(chan error, 1)
	arguments := mustArguments(t, `{"path":"input.txt"}`)

	go func() {
		events, executeErr := runtime.executeProviderToolCalls(
			registry, uuid.Must(uuid.NewV7()).String(), directory,
		)(t.Context(), []ToolCall{{
			Metadata: nil, ArgumentsJSON: "", ID: "foreground-call", Name: string(tool.NameRead), Arguments: arguments,
		}}, nil)
		errs <- executeErr

		done <- events
	}()

	<-controller.started
	close(controller.done)
	events := <-done

	require.NoError(t, <-errs)
	require.Len(t, events, 1)
	assert.Equal(t, "ordinary result", events[0].Result)
	assert.NotContains(t, events[0].Result, controller.entity.Task.ID)

	_, attached := runtime.DetachForegroundTool("foreground-call")
	assert.False(t, attached)
}

func TestForegroundToolDetachReturnsSameDurableHandleExactlyOnce(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	controller := newDetachableTaskController(t, tool.TextResult("later", nil))
	runtime := newDetachTestRuntime(controller)
	registry := tool.NewRegistry(directory)
	events := make(chan StreamEvent, 4)
	done := make(chan []ToolEvent, 1)
	errs := make(chan error, 1)
	arguments := mustArguments(t, `{"path":"missing"}`)

	go func() {
		results, executeErr := runtime.executeProviderToolCalls(
			registry, uuid.Must(uuid.NewV7()).String(), directory,
		)(t.Context(), []ToolCall{{
			Metadata: nil, ArgumentsJSON: "", ID: "detach-call", Name: string(tool.NameRead), Arguments: arguments,
		}}, func(event StreamEvent) { events <- event })
		errs <- executeErr

		done <- results
	}()

	<-controller.started
	require.Eventually(t, func() bool {
		runtime.attachments.mu.Lock()
		defer runtime.attachments.mu.Unlock()

		return runtime.attachments.items["detach-call"] != nil
	}, time.Second, time.Millisecond)

	taskID, detached := runtime.DetachForegroundTool("detach-call")
	require.True(t, detached)
	assert.Equal(t, controller.entity.Task.ID, taskID)

	results := <-done

	require.NoError(t, <-errs)
	require.Len(t, results, 1)
	assert.Contains(t, results[0].Result, taskID)

	close(controller.done)

	resultEvents := 0

	for len(events) > 0 {
		if (<-events).Kind == StreamEventToolResult {
			resultEvents++
		}
	}

	assert.Equal(t, 1, resultEvents)
}

func TestForegroundToolCompletionDetachRaceHasOneConsistentWinner(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	arguments := mustArguments(t, `{"path":"input.txt"}`)

	for range 50 {
		controller := newDetachableTaskController(t, tool.TextResult("ordinary", nil))
		runtime := newDetachTestRuntime(controller)
		results := make(chan []ToolEvent, 1)
		errs := make(chan error, 1)

		go func() {
			execute := runtime.executeProviderToolCalls(
				tool.NewRegistry(directory), uuid.Must(uuid.NewV7()).String(), directory,
			)

			events, executeErr := execute(t.Context(), []ToolCall{{
				Metadata: nil, ArgumentsJSON: "", ID: "race-call", Name: string(tool.NameRead), Arguments: arguments,
			}}, nil)
			errs <- executeErr

			results <- events
		}()

		<-controller.started
		require.Eventually(t, func() bool {
			runtime.attachments.mu.Lock()
			defer runtime.attachments.mu.Unlock()

			return runtime.attachments.items["race-call"] != nil
		}, time.Second, time.Millisecond)

		start := make(chan struct{})
		detached := make(chan bool, 1)

		go func() {
			<-start

			_, won := runtime.DetachForegroundTool("race-call")
			detached <- won
		}()
		go func() { <-start; close(controller.done) }()

		close(start)

		won := <-detached
		events := <-results

		require.NoError(t, <-errs)
		require.Len(t, events, 1)

		if won {
			assert.Contains(t, events[0].Result, controller.entity.Task.ID)
		} else {
			assert.Equal(t, "ordinary", events[0].Result)
		}
	}
}

func newDetachTestRuntime(controller ToolTaskController) *Runtime {
	return NewRuntime(&RuntimeOptions{
		Config: nil, Sessions: nil, Extensions: nil, Cache: nil, Models: nil, Client: nil, Logger: nil,
		SkillsCache: nil, Agents: nil, AgentTasks: nil, WorkflowSubmitter: nil,
		ToolTasks: controller, ToolCoordinator: nil,
	})
}

func mustArguments(t *testing.T, value string) tool.Arguments {
	t.Helper()

	arguments, err := tool.ArgumentsFromRaw([]byte(value))
	require.NoError(t, err)

	return arguments
}
