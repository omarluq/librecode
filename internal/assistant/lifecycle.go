package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/assistant/lifecyclepayload"
	"github.com/omarluq/librecode/internal/contextwindow"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/model"
)

type promptTurnLifecycle struct {
	runtime     *Runtime
	sessionID   string
	userEntryID string
	ended       bool
}

func newPromptTurnLifecycle(
	runtime *Runtime,
	sessionID string,
	userEntryID string,
) *promptTurnLifecycle {
	return &promptTurnLifecycle{
		runtime:     runtime,
		sessionID:   sessionID,
		userEntryID: userEntryID,
		ended:       false,
	}
}

func (lifecycle *promptTurnLifecycle) dispatchEnd(
	ctx context.Context,
	assistantEntryID string,
	cached bool,
	usage model.TokenUsage,
) {
	if lifecycle == nil || lifecycle.ended {
		return
	}

	lifecycle.ended = true
	lifecycle.runtime.dispatchTurnEndLifecycle(
		ctx,
		lifecycle.sessionID,
		lifecycle.userEntryID,
		assistantEntryID,
		cached,
		usage,
	)
}

func (lifecycle *promptTurnLifecycle) dispatchError(ctx context.Context, err error) {
	if lifecycle == nil || lifecycle.ended || err == nil {
		return
	}

	lifecycle.ended = true
	lifecycle.runtime.dispatchTurnErrorLifecycle(ctx, lifecycle.sessionID, lifecycle.userEntryID, err)
}

func (runtime *Runtime) dispatchLifecycle(
	ctx context.Context,
	name extension.LifecycleEventName,
	payload map[string]any,
) (extension.LifecycleDispatchResult, error) {
	runtime.emit(ctx, string(name), payload)

	if runtime.extensions == nil {
		return emptyLifecycleDispatchResult(name, payload), nil
	}

	result, err := runtime.extensions.DispatchLifecycle(ctx, extension.LifecycleEvent{
		Payload: payload,
		Name:    name,
	})
	if err != nil && runtime.logger != nil {
		runtime.logger.Debug(
			"extension lifecycle event failed",
			slog.String("event", string(name)),
			slog.Any("error", err),
		)
	}

	return result, assistantError(err, "dispatch lifecycle event")
}

func (runtime *Runtime) dispatchMessageAppend(ctx context.Context, entry *database.EntryEntity) {
	if entry == nil {
		return
	}

	runtime.dispatchObservationalLifecycle(ctx, extension.LifecycleMessageAppend, lifecyclepayload.Entry(entry))
}

func (runtime *Runtime) dispatchTurnStartLifecycle(
	ctx context.Context,
	sessionID string,
	request *PromptRequest,
	userEntryID string,
	parentEntryID *string,
) {
	payload := lifecyclepayload.TurnStart(sessionID, request.CWD, request.Text, userEntryID, parentEntryID)
	runtime.dispatchObservationalLifecycle(ctx, extension.LifecycleBeforeAgentStart, payload)
	runtime.dispatchObservationalLifecycle(ctx, extension.LifecycleAgentStart, payload)
	runtime.dispatchObservationalLifecycle(ctx, extension.LifecycleTurnStart, payload)
}

func (runtime *Runtime) dispatchTurnEndLifecycle(
	ctx context.Context,
	sessionID string,
	userEntryID string,
	assistantEntryID string,
	cached bool,
	usage model.TokenUsage,
) {
	payload := lifecyclepayload.TurnEndPayload(&lifecyclepayload.TurnEnd{
		Err:              nil,
		Usage:            usage,
		AssistantEntryID: assistantEntryID,
		SessionID:        sessionID,
		UserEntryID:      userEntryID,
		Cached:           cached,
	})
	runtime.dispatchObservationalLifecycle(ctx, extension.LifecycleTurnEnd, payload)
	runtime.dispatchObservationalLifecycle(ctx, extension.LifecycleAgentEnd, payload)
}

func (runtime *Runtime) dispatchTurnErrorLifecycle(
	ctx context.Context,
	sessionID string,
	userEntryID string,
	turnErr error,
) {
	if turnErr == nil {
		return
	}

	payload := lifecyclepayload.TurnEndPayload(&lifecyclepayload.TurnEnd{
		Err:              turnErr,
		Usage:            model.EmptyTokenUsage(),
		AssistantEntryID: "",
		SessionID:        sessionID,
		UserEntryID:      userEntryID,
		Cached:           false,
	})
	runtime.dispatchObservationalLifecycle(ctx, extension.LifecycleTurnEnd, payload)
	runtime.dispatchObservationalLifecycle(ctx, extension.LifecycleAgentEnd, payload)
}

func (runtime *Runtime) dispatchObservationalLifecycle(
	ctx context.Context,
	name extension.LifecycleEventName,
	payload map[string]any,
) {
	if _, err := runtime.dispatchLifecycle(ctx, name, payload); err != nil {
		return
	}
}

func (runtime *Runtime) dispatchContextBuild(
	ctx context.Context,
	sessionID string,
	cwd string,
	base *contextwindow.Base,
	result *contextwindow.BuildResult,
) (extension.LifecycleDispatchResult, error) {
	payload := lifecyclepayload.ContextBuild(sessionID, cwd, base, result)

	return runtime.dispatchLifecycle(ctx, extension.LifecycleContextBuild, payload)
}

func emptyLifecycleDispatchResult(
	name extension.LifecycleEventName,
	payload map[string]any,
) extension.LifecycleDispatchResult {
	return extension.LifecycleDispatchResult{
		Payload:         cloneAnyMap(payload),
		ProviderRequest: extension.ProviderRequestMutation{Headers: map[string]string{}},
		ToolCall:        extension.ToolCallMutation{Arguments: nil},
		ToolResult:      extension.ToolResultMutation{Result: nil, DetailsJSON: nil, Error: nil},
		Compaction: extension.CompactionMutation{
			Summary:          nil,
			FirstKeptEntryID: nil,
			Details:          nil,
			Cancel:           false,
		},
		Name:         string(name),
		Errors:       []string{},
		Duration:     0,
		HandlerCount: 0,
		Consumed:     false,
		Stopped:      false,
	}
}

func (runtime *Runtime) dispatchToolCallLifecycle(ctx context.Context, call *ToolCallEvent) error {
	if call == nil {
		return nil
	}

	payload := lifecyclepayload.ToolCallPayload(lifecycleToolCall(*call))

	result, err := runtime.dispatchLifecycle(ctx, extension.LifecycleToolCall, payload)
	if err != nil {
		runtime.emitLifecycleDiagnostics(ctx, extension.LifecycleToolCall, &result, toolCallDiagnostics(call))

		return err
	}

	if err := applyToolCallMutation(call, result.ToolCall); err != nil {
		runtime.emitLifecycleDiagnostics(ctx, extension.LifecycleToolCall, &result, toolCallDiagnostics(call))

		return err
	}

	runtime.emitLifecycleDiagnostics(ctx, extension.LifecycleToolCall, &result, toolCallDiagnostics(call))

	return nil
}

func (runtime *Runtime) dispatchToolResultLifecycle(ctx context.Context, event *ToolEvent) error {
	if event == nil {
		return nil
	}

	payload := lifecyclepayload.ToolResultPayload(lifecycleToolResult(event))

	result, err := runtime.dispatchLifecycle(ctx, extension.LifecycleToolResult, payload)
	if err != nil {
		runtime.emitLifecycleDiagnostics(ctx, extension.LifecycleToolResult, &result, toolResultDiagnostics(event))

		return err
	}

	applyToolResultMutation(event, result.ToolResult)
	runtime.emitLifecycleDiagnostics(ctx, extension.LifecycleToolResult, &result, toolResultDiagnostics(event))

	if event.Error != "" {
		runtime.dispatchToolErrorLifecycle(ctx, event)
	}

	return nil
}

func (runtime *Runtime) dispatchToolErrorLifecycle(ctx context.Context, event *ToolEvent) {
	if event == nil || event.Error == "" {
		return
	}

	payload := lifecyclepayload.ToolResultPayload(lifecycleToolResult(event))
	result, err := runtime.dispatchLifecycle(ctx, extension.LifecycleToolError, payload)
	runtime.emitLifecycleDiagnostics(ctx, extension.LifecycleToolError, &result, toolResultDiagnostics(event))

	if err != nil {
		return
	}
}

func applyToolCallMutation(call *ToolCallEvent, mutation extension.ToolCallMutation) error {
	if len(mutation.Arguments) == 0 {
		return nil
	}

	argumentsJSON, err := encodeToolArguments(mutation.Arguments)
	if err != nil {
		return oops.In("assistant").Code("tool_call_arguments").Wrapf(err, "encode mutated tool arguments")
	}

	call.Arguments = mutation.Arguments
	call.ArgumentsJSON = argumentsJSON

	return nil
}

func encodeToolArguments(arguments map[string]any) (string, error) {
	if len(arguments) == 0 {
		return "{}", nil
	}

	encoded, err := json.Marshal(arguments)
	if err != nil {
		return "", fmt.Errorf("marshal tool arguments: %w", err)
	}

	return string(encoded), nil
}

func applyToolResultMutation(event *ToolEvent, mutation extension.ToolResultMutation) {
	if mutation.Result != nil {
		event.Result = *mutation.Result
	}

	if mutation.DetailsJSON != nil {
		event.DetailsJSON = *mutation.DetailsJSON
	}

	if mutation.Error != nil {
		event.Error = *mutation.Error
		event.IsError = strings.TrimSpace(*mutation.Error) != ""
	}
}

func lifecyclePromptRequest(request *PromptRequest) *lifecyclepayload.PromptRequest {
	if request == nil {
		return &lifecyclepayload.PromptRequest{
			ParentEntryID: nil,
			CWD:           "",
			Name:          "",
			SessionID:     "",
			Text:          "",
			ResumeLatest:  false,
		}
	}

	return &lifecyclepayload.PromptRequest{
		ParentEntryID: request.ParentEntryID,
		CWD:           request.CWD,
		Name:          request.Name,
		SessionID:     request.SessionID,
		Text:          request.Text,
		ResumeLatest:  request.ResumeLatest,
	}
}

func lifecycleToolCall(call ToolCallEvent) lifecyclepayload.ToolCall {
	return lifecyclepayload.ToolCall{
		Arguments:     call.Arguments,
		ID:            call.ID,
		Name:          call.Name,
		ArgumentsJSON: call.ArgumentsJSON,
	}
}

func lifecycleToolResult(event *ToolEvent) *lifecyclepayload.ToolResult {
	return &lifecyclepayload.ToolResult{
		Name:          event.Name,
		ArgumentsJSON: event.ArgumentsJSON,
		DetailsJSON:   event.DetailsJSON,
		Result:        event.Result,
		Error:         event.Error,
		IsError:       event.IsError,
	}
}
