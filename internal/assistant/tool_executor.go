package assistant

import (
	"context"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/provider"
	"github.com/omarluq/librecode/internal/tool"
)

func (runtime *Runtime) executeProviderToolCalls(
	registry *tool.Registry,
) provider.ToolExecutor {
	return func(
		ctx context.Context,
		calls []provider.ToolCall,
		onEvent func(provider.StreamEvent),
	) ([]provider.ToolEvent, error) {
		if registry == nil {
			return nil, oops.In("assistant").Code("tool_registry_missing").Errorf("tool registry is not configured")
		}

		events := make([]provider.ToolEvent, 0, len(calls))
		for _, call := range calls {
			events = append(events, runtime.executeProviderToolCall(ctx, registry, call, onEvent))
		}

		return events, nil
	}
}

func (runtime *Runtime) executeProviderToolCall(
	ctx context.Context,
	registry *tool.Registry,
	call provider.ToolCall,
	onEvent func(provider.StreamEvent),
) provider.ToolEvent {
	emitProviderToolStart(onEvent, call.Name)
	callEvent := provider.ToolCallEvent{
		Arguments:     call.Arguments,
		ID:            call.ID,
		Name:          call.Name,
		ArgumentsJSON: call.ArgumentsJSON,
	}
	if err := runtime.dispatchToolCallLifecycle(ctx, &callEvent); err != nil {
		event := provider.ToolEvent{
			Name:          callEvent.Name,
			ArgumentsJSON: callEvent.ArgumentsJSON,
			DetailsJSON:   "",
			Result:        err.Error(),
			Error:         err.Error(),
			IsError:       true,
		}
		emitProviderToolResult(onEvent, &event)

		return event
	}

	result, err := registry.Execute(ctx, callEvent.Name, callEvent.Arguments)
	event := provider.ToolEventFromResult(callEvent, result, err)
	if lifecycleErr := runtime.dispatchToolResultLifecycle(ctx, &event); lifecycleErr != nil {
		event.Error = lifecycleErr.Error()
		event.Result = lifecycleErr.Error()
		event.IsError = true
	}
	emitProviderToolResult(onEvent, &event)

	return event
}

func emitProviderToolStart(onEvent func(provider.StreamEvent), name string) {
	if onEvent == nil {
		return
	}
	onEvent(provider.StreamEvent{
		ToolEvent: nil,
		Usage:     nil,
		Kind:      provider.StreamEventToolStart,
		Text:      name,
	})
}

func emitProviderToolResult(onEvent func(provider.StreamEvent), event *provider.ToolEvent) {
	if onEvent == nil {
		return
	}
	onEvent(provider.StreamEvent{
		ToolEvent: event,
		Usage:     nil,
		Kind:      provider.StreamEventToolResult,
		Text:      "",
	})
}
