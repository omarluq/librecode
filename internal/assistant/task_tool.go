package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/tool"
	"github.com/omarluq/librecode/internal/tooltask"
)

const (
	taskIDKey               = "task_id"
	taskStateKey            = "state"
	backgroundActionGet     = "get"
	backgroundActionCancel  = "cancel"
	additionalPropertiesKey = "additionalProperties"
	backgroundKey           = "background"
	requiredKey             = "required"
)

// ToolTaskController is the transport-neutral assistant boundary for durable tool work.
type ToolTaskController interface {
	Start(ctx context.Context, request *tooltask.StartRequest) (*database.ToolTaskEntity, error)
	Get(ctx context.Context, owner, taskID string) (*database.ToolTaskEntity, bool, error)
	List(
		ctx context.Context, owner string, states []database.TaskState, limit int,
	) ([]database.ToolTaskEntity, error)
	Cancel(ctx context.Context, owner, taskID string) (*database.ToolTaskEntity, bool, error)
	Wait(ctx context.Context, owner, taskID string) (*database.ToolTaskEntity, error)
}

type backgroundToolExecutor struct {
	target     tool.Executor
	controller ToolTaskController
	admit      func(context.Context, *tooltask.StartRequest) error
	cache      *backgroundDefinitionCache
	owner      string
	cwd        string
}

type backgroundDefinitionCache struct {
	definition tool.Definition
	once       sync.Once
}

type backgroundEnvelope struct {
	TaskID         string          `json:"task_id"`
	Action         string          `json:"action"`
	Arguments      json.RawMessage `json:"arguments"`
	TimeoutSeconds int             `json:"timeout_seconds"`
}

type backgroundEnvelopeInput struct {
	Background backgroundEnvelope `json:"background"`
}

func (executor *backgroundToolExecutor) Definition() tool.Definition {
	executor.cache.once.Do(func() {
		executor.cache.definition = executor.buildDefinition()
	})

	return executor.cache.definition
}

func (executor *backgroundToolExecutor) buildDefinition() tool.Definition {
	definition := executor.target.Definition()

	ordinary := definition.Schema.RawMessage()
	if len(ordinary) == 0 {
		ordinary = json.RawMessage(`{"type":"object"}`)
	}

	var ordinarySchema any
	if err := json.Unmarshal(ordinary, &ordinarySchema); err != nil {
		panic(err)
	}

	schema := map[string]any{
		jsonTypeKey: jsonObjectType,
		"oneOf": []any{
			ordinarySchema,
			map[string]any{
				jsonTypeKey: jsonObjectType, additionalPropertiesKey: false, requiredKey: []string{backgroundKey},
				jsonPropertiesKey: map[string]any{backgroundKey: map[string]any{
					jsonTypeKey: jsonObjectType, additionalPropertiesKey: false, requiredKey: []string{"arguments"},
					jsonPropertiesKey: map[string]any{
						"arguments":       ordinarySchema,
						"timeout_seconds": map[string]any{jsonTypeKey: "integer", "minimum": 1},
					},
				}},
			},
			map[string]any{
				jsonTypeKey: jsonObjectType, additionalPropertiesKey: false, requiredKey: []string{backgroundKey},
				jsonPropertiesKey: map[string]any{backgroundKey: map[string]any{
					jsonTypeKey: jsonObjectType, additionalPropertiesKey: false,
					requiredKey: []string{"action", "task_id"},
					jsonPropertiesKey: map[string]any{
						"action": map[string]any{
							jsonTypeKey: "string", "enum": []string{backgroundActionGet, backgroundActionCancel},
						},
						"task_id": map[string]any{jsonTypeKey: "string"},
					},
				}},
			},
		},
	}

	encoded, err := json.Marshal(schema)
	if err != nil {
		panic(err)
	}

	definition.Schema, err = tool.SchemaFromRaw(encoded)
	if err != nil {
		panic(err)
	}

	return definition
}

func (executor *backgroundToolExecutor) Execute(ctx context.Context, input tool.Arguments) (tool.Result, error) {
	if !input.HasField(backgroundKey) {
		result, err := executor.target.Execute(ctx, input)
		if err != nil {
			return result, oops.In("assistant").Code("execute_foreground_tool").Wrapf(err, "execute foreground tool")
		}

		return result, nil
	}

	var envelope backgroundEnvelopeInput
	if err := input.Decode(&envelope); err != nil {
		return tool.Result{}, oops.In("assistant").Code("decode_background_tool").
			Wrapf(err, "decode background tool input")
	}

	if envelope.Background.Action != "" {
		return executor.manage(ctx, envelope.Background.Action, envelope.Background.TaskID)
	}

	return executor.start(ctx, &envelope.Background)
}

func (executor *backgroundToolExecutor) start(
	ctx context.Context,
	background *backgroundEnvelope,
) (tool.Result, error) {
	arguments, err := tool.ArgumentsFromRaw(background.Arguments)
	if err != nil {
		return tool.Result{}, oops.In("assistant").Code("parse_background_arguments").
			Wrapf(err, "parse background arguments")
	}

	invocation, ok := taskInvocationFromContext(ctx)
	if !ok {
		return tool.Result{}, oops.In("assistant").Code("background_invocation_missing").
			Errorf("background execution requires assistant invocation metadata")
	}

	invocation.OwnerSessionID = executor.owner
	invocation.CWD = executor.cwd
	invocation.WrapperCallID = invocation.ID
	invocation.ParentCallID = invocation.ID
	invocation.ID += "/target"

	if background.TimeoutSeconds < 0 {
		return tool.Result{}, oops.In("assistant").Code("background_timeout_invalid").
			Errorf("timeout_seconds must be positive")
	}

	maxTimeoutSeconds := int64(time.Duration(1<<63-1) / time.Second)
	if int64(background.TimeoutSeconds) > maxTimeoutSeconds {
		return tool.Result{}, oops.In("assistant").Code("background_timeout_overflow").
			Errorf("timeout_seconds is too large")
	}

	created, err := executor.controller.Start(ctx, &tooltask.StartRequest{
		Invocation: invocation,
		Target:     string(executor.target.Definition().Name),
		Arguments:  arguments,
		Timeout:    time.Duration(background.TimeoutSeconds) * time.Second,
		Admit:      executor.admit,
	})
	if err != nil {
		return tool.Result{}, oops.In("assistant").Code("start_tool_task").Wrapf(err, "start tool task")
	}

	return taskEntityResult(created, false), nil
}

func (executor *backgroundToolExecutor) manage(ctx context.Context, action, taskID string) (tool.Result, error) {
	parsed, err := uuid.FromString(taskID)
	if err != nil || parsed.Version() != 7 || parsed.String() != taskID {
		return tool.Result{}, oops.In("assistant").Code("background_task_id_invalid").
			Errorf("task_id must be a canonical UUIDv7")
	}

	entity, err := executor.ownedTargetTask(ctx, taskID)
	if err != nil {
		return tool.Result{}, err
	}

	switch action {
	case backgroundActionGet:
		return taskEntityResult(entity, true), nil
	case backgroundActionCancel:
		entity, found, err := executor.controller.Cancel(ctx, executor.owner, taskID)
		if err != nil {
			return tool.Result{}, oops.In("assistant").Code("cancel_tool_task").Wrapf(err, "cancel tool task")
		}

		if !found {
			return tool.Result{}, oops.In("assistant").Code("tool_task_not_found").
				Errorf("task %q not found", taskID)
		}

		return taskEntityResult(entity, false), nil
	default:
		return tool.Result{}, oops.In("assistant").Code("background_action_invalid").
			Errorf("unknown background action %q", action)
	}
}

func (executor *backgroundToolExecutor) ownedTargetTask(
	ctx context.Context,
	taskID string,
) (*database.ToolTaskEntity, error) {
	entity, found, err := executor.controller.Get(ctx, executor.owner, taskID)
	if err != nil {
		return nil, oops.In("assistant").Code("get_tool_task").Wrapf(err, "get tool task")
	}

	if !found {
		return nil, oops.In("assistant").Code("tool_task_not_found").Errorf("task %q not found", taskID)
	}

	targetName := string(executor.target.Definition().Name)
	if entity.TargetName != targetName {
		return nil, oops.In("assistant").Code("tool_task_target_mismatch").
			Errorf("task %q belongs to tool %q, not %q", taskID, entity.TargetName, targetName)
	}

	return entity, nil
}

func (executor *backgroundToolExecutor) Sequential() bool {
	sequential, ok := executor.target.(interface{ Sequential() bool })

	return ok && sequential.Sequential()
}

func taskEntityResult(entity *database.ToolTaskEntity, includeOutcome bool) tool.Result {
	details := taskDetails(entity)

	text := fmt.Sprintf("task %s (%s) is %s", entity.Task.ID, entity.TargetName, entity.Task.State)
	if includeOutcome && entity.OutcomeJSON != nil {
		text = *entity.OutcomeJSON

		var outcome any
		if json.Unmarshal([]byte(*entity.OutcomeJSON), &outcome) == nil {
			details["outcome"] = outcome
		}
	}

	return tool.TextResult(text, details)
}

func taskDetails(entity *database.ToolTaskEntity) map[string]any {
	return map[string]any{
		taskIDKey: entity.Task.ID, "kind": entity.Task.Kind, taskStateKey: entity.Task.State,
		"created_at": entity.Task.CreatedAt, "updated_at": entity.Task.UpdatedAt,
		"error_code": entity.Task.ErrorCode, "error_message": entity.Task.ErrorMessage,
		"target": entity.TargetName,
	}
}

type taskInvocationContextKey struct{}

func withTaskInvocation(ctx context.Context, invocation *tooltask.Invocation) context.Context {
	return context.WithValue(ctx, taskInvocationContextKey{}, invocation)
}

func taskInvocationFromContext(ctx context.Context) (tooltask.Invocation, bool) {
	value, ok := ctx.Value(taskInvocationContextKey{}).(*tooltask.Invocation)
	if !ok || value == nil {
		return tooltask.Invocation{
			ID: "", WrapperCallID: "", ParentCallID: "", OwnerSessionID: "", CWD: "",
			InitiatingEntryID: "", SourceSequence: 0,
		}, false
	}

	return *value, true
}
