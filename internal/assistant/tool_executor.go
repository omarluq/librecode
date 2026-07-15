package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/tool"
)

func (runtime *Runtime) executeProviderToolCalls(
	registry *tool.Registry,
) ToolExecutor {
	return func(
		ctx context.Context,
		calls []ToolCall,
		onEvent func(StreamEvent),
	) ([]ToolEvent, error) {
		if registry == nil {
			return nil, oops.In("assistant").Code("tool_registry_missing").Errorf("tool registry is not configured")
		}

		events := make([]ToolEvent, 0, len(calls))
		for index := range calls {
			events = append(events, runtime.executeProviderToolCall(ctx, registry, &calls[index], onEvent))
		}

		return events, nil
	}
}

type toolInvocationScope struct {
	onEvent      func(StreamEvent)
	trace        func(name, argumentsJSON string, result *tool.Result, event *ToolEvent)
	parentCallID string
	nextSequence int
	mu           sync.Mutex
}

func (scope *toolInvocationScope) nextCall(name string) ToolCallEvent {
	scope.mu.Lock()
	defer scope.mu.Unlock()

	scope.nextSequence++
	sequence := scope.nextSequence

	callID := fmt.Sprintf("%s/%d", scope.parentCallID, sequence)
	if scope.parentCallID == "" {
		callID = fmt.Sprintf("nested/%d", sequence)
	}

	return ToolCallEvent{
		ArgumentsJSON: "",
		ID:            callID,
		ParentCallID:  scope.parentCallID,
		Name:          name,
		Arguments:     tool.EmptyArguments(),
		Sequence:      sequence,
	}
}

type toolInvocationContextKey struct{}

func (runtime *Runtime) executeProviderToolCall(
	ctx context.Context,
	registry *tool.Registry,
	call *ToolCall,
	onEvent func(StreamEvent),
) ToolEvent {
	callEvent := ToolCallEvent{
		ArgumentsJSON: "",
		ID:            "",
		ParentCallID:  "",
		Name:          "",
		Arguments:     tool.EmptyArguments(),
		Sequence:      0,
	}
	if call != nil {
		callEvent.Arguments = call.Arguments
		callEvent.ID = call.ID
		callEvent.Name = call.Name
		callEvent.ArgumentsJSON = call.ArgumentsJSON
	}

	event, _ := runtime.invokeToolResult(ctx, registry, &callEvent, onEvent)

	return event
}

func (runtime *Runtime) invokeToolResult(
	ctx context.Context,
	registry *tool.Registry,
	callEvent *ToolCallEvent,
	onEvent func(StreamEvent),
) (ToolEvent, tool.Result) {
	if err := runtime.dispatchToolCallLifecycle(ctx, callEvent); err != nil {
		event := toolLifecycleErrorEvent(callEvent, err)
		emitProviderToolResult(onEvent, &event)

		return event, tool.Result{Content: nil, Details: nil}
	}

	emitProviderToolStart(onEvent, callEvent)

	scope := &toolInvocationScope{
		onEvent: onEvent, trace: nil, parentCallID: callEvent.ID, nextSequence: 0, mu: sync.Mutex{},
	}
	nestedCtx := context.WithValue(ctx, toolInvocationContextKey{}, scope)
	result, err := registry.Execute(nestedCtx, callEvent.Name, callEvent.Arguments)

	event := toolEventFromResult(callEvent, result, err)
	if lifecycleErr := runtime.dispatchToolResultLifecycle(ctx, &event); lifecycleErr != nil && runtime.logger != nil {
		runtime.logger.Debug("tool result lifecycle failed", "error", lifecycleErr)
	}

	emitProviderToolResult(onEvent, &event)

	return event, result
}

func (runtime *Runtime) invokeNestedTool(
	ctx context.Context,
	registry *tool.Registry,
	name string,
	arguments tool.Arguments,
	argumentsJSON string,
) (tool.Result, ToolEvent) {
	scope := toolInvocationScopeFromContext(ctx)
	call := ToolCallEvent{
		ArgumentsJSON: argumentsJSON,
		ID:            "",
		ParentCallID:  "",
		Name:          name,
		Arguments:     arguments,
		Sequence:      0,
	}

	var onEvent func(StreamEvent)

	if scope != nil {
		identity := scope.nextCall(name)
		call.ID = identity.ID
		call.ParentCallID = identity.ParentCallID
		call.Sequence = identity.Sequence
		onEvent = scope.onEvent
	}

	event, result := runtime.invokeToolResult(ctx, registry, &call, onEvent)
	if scope != nil && scope.trace != nil {
		scope.trace(name, argumentsJSON, &result, &event)
	}

	return result, event
}

func toolInvocationScopeFromContext(ctx context.Context) *toolInvocationScope {
	scope, ok := ctx.Value(toolInvocationContextKey{}).(*toolInvocationScope)
	if !ok {
		return nil
	}

	return scope
}

func toolLifecycleErrorEvent(call *ToolCallEvent, err error) ToolEvent {
	return ToolEvent{
		CallID:        call.ID,
		ParentCallID:  call.ParentCallID,
		Name:          call.Name,
		ArgumentsJSON: call.ArgumentsJSON,
		DetailsJSON:   "",
		Result:        err.Error(),
		Error:         err.Error(),
		Sequence:      call.Sequence,
		IsError:       true,
	}
}

func toolEventFromResult(call *ToolCallEvent, result tool.Result, err error) ToolEvent {
	resultText := result.Text()
	detailsJSON := encodeToolDetails(result.Details)

	if err != nil {
		resultText = err.Error()
	}

	if strings.TrimSpace(resultText) == "" {
		resultText = "(tool returned no text output)"
	}

	event := ToolEvent{
		CallID:        call.ID,
		ParentCallID:  call.ParentCallID,
		Name:          call.Name,
		ArgumentsJSON: call.ArgumentsJSON,
		DetailsJSON:   detailsJSON,
		Result:        resultText,
		Error:         "",
		Sequence:      call.Sequence,
		IsError:       false,
	}
	if err != nil {
		event.Error = err.Error()
		event.IsError = true
	}

	return event
}

func encodeToolDetails(details map[string]any) string {
	if len(details) == 0 {
		return ""
	}

	encoded, err := json.Marshal(details)
	if err != nil {
		return ""
	}

	return string(encoded)
}

func emitProviderToolStart(onEvent func(StreamEvent), call *ToolCallEvent) {
	if onEvent == nil || call == nil {
		return
	}

	onEvent(StreamEvent{
		ToolCallEvent: call,
		ToolEvent:     nil,
		Usage:         nil,
		Kind:          StreamEventToolStart,
		Text:          call.Name,
	})
}

func emitProviderToolResult(onEvent func(StreamEvent), event *ToolEvent) {
	if onEvent == nil {
		return
	}

	onEvent(StreamEvent{ToolCallEvent: nil, ToolEvent: event, Usage: nil, Kind: StreamEventToolResult, Text: ""})
}

func llmToolResultFromToolEvent(event *ToolEvent) llm.ToolResult {
	if event == nil {
		return llm.ToolResult{
			Metadata:      nil,
			ToolCallID:    "",
			ArgumentsJSON: "",
			Name:          "",
			Error:         "",
			Content:       nil,
			IsError:       false,
		}
	}

	return llm.ToolResult{
		Metadata:      toolResultMetadataFromToolEvent(event),
		ToolCallID:    event.CallID,
		ArgumentsJSON: event.ArgumentsJSON,
		Name:          event.Name,
		Error:         event.Error,
		Content:       []llm.Part{llm.TextPart(event.Result)},
		IsError:       event.IsError,
	}
}

func toolResultMetadataFromToolEvent(event *ToolEvent) map[string]any {
	if event == nil {
		return nil
	}

	metadata := toolIdentityMetadata(event.ParentCallID, event.Sequence)
	if strings.TrimSpace(event.DetailsJSON) == "" {
		return metadata
	}

	if metadata == nil {
		metadata = make(map[string]any, 1)
	}

	metadata["details_json"] = event.DetailsJSON

	return metadata
}
