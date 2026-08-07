package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/gofrs/uuid/v5"
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

		prepared := runtime.prepareProviderToolCalls(ctx, registry, calls)

		return runtime.runPreparedToolCalls(ctx, prepared, onEvent), nil
	}
}

type preparedToolCall struct {
	err               error
	call              *ToolCallEvent
	invocation        tool.PreparedCall
	result            tool.Result
	lifecycleRejected bool
	ready             bool
}

func (runtime *Runtime) prepareProviderToolCalls(
	ctx context.Context,
	registry *tool.Registry,
	calls []ToolCall,
) []preparedToolCall {
	prepared := make([]preparedToolCall, len(calls))
	for index := range calls {
		prepared[index] = runtime.prepareProviderToolCall(ctx, registry, &calls[index])
	}

	return prepared
}

func (runtime *Runtime) runPreparedToolCalls(
	ctx context.Context,
	prepared []preparedToolCall,
	onEvent func(StreamEvent),
) []ToolEvent {
	defer releasePreparedToolCalls(prepared)

	return runtime.executePreparedToolCalls(ctx, prepared, onEvent)
}

func releasePreparedToolCalls(calls []preparedToolCall) {
	for index := range calls {
		calls[index].invocation.Release()
	}
}

func (runtime *Runtime) prepareProviderToolCall(
	ctx context.Context,
	registry *tool.Registry,
	call *ToolCall,
) preparedToolCall {
	callEvent := toolCallEvent(call)

	prepared := preparedToolCall{
		err:               nil,
		call:              &callEvent,
		invocation:        tool.PreparedCall{},
		result:            tool.Result{Details: nil, Content: nil},
		lifecycleRejected: false,
		ready:             false,
	}
	if err := runtime.dispatchToolCallLifecycle(ctx, prepared.call); err != nil {
		prepared.err = err
		prepared.lifecycleRejected = true

		return prepared
	}

	prepared.invocation, prepared.err = registry.Prepare(prepared.call.Name, prepared.call.Arguments)
	prepared.ready = prepared.err == nil

	return prepared
}

func (runtime *Runtime) executePreparedToolCalls(
	ctx context.Context,
	calls []preparedToolCall,
	onEvent func(StreamEvent),
) []ToolEvent {
	for index := range calls {
		if !calls[index].lifecycleRejected {
			emitProviderToolStart(onEvent, calls[index].call)
		}

		if calls[index].ready {
			calls[index].invocation.Admit()
		}
	}

	events := make([]ToolEvent, len(calls))
	if batchRequiresSequentialExecution(calls) {
		for index := range calls {
			runtime.executePreparedToolCall(ctx, &calls[index], onEvent)
			events[index] = runtime.finalizePreparedToolCall(ctx, &calls[index], onEvent)
		}

		return events
	}

	guardedEvent := serializedEventSink(onEvent)

	completed := make(chan int, len(calls))
	// Provider tool batches intentionally have no process-local concurrency cap.
	// Resource-specific limits can be added later without changing batch semantics.
	for index := range calls {
		if !calls[index].ready {
			completed <- index

			continue
		}

		go func(index int) {
			runtime.executePreparedToolCall(ctx, &calls[index], guardedEvent)

			completed <- index
		}(index)
	}

	for range calls {
		index := <-completed
		events[index] = runtime.finalizePreparedToolCall(ctx, &calls[index], guardedEvent)
	}

	return events
}

func batchRequiresSequentialExecution(calls []preparedToolCall) bool {
	for index := range calls {
		if calls[index].ready && calls[index].invocation.Sequential() {
			return true
		}
	}

	return false
}

func serializedEventSink(onEvent func(StreamEvent)) func(StreamEvent) {
	if onEvent == nil {
		return nil
	}

	var mu sync.Mutex

	return func(event StreamEvent) {
		mu.Lock()
		defer mu.Unlock()

		onEvent(event)
	}
}

func (*Runtime) executePreparedToolCall(
	ctx context.Context,
	prepared *preparedToolCall,
	onEvent func(StreamEvent),
) {
	if prepared == nil || !prepared.ready {
		return
	}

	scope := &toolInvocationScope{
		onEvent: onEvent, parentCallID: prepared.call.ID, nextSequence: 0, mu: sync.Mutex{},
	}
	nestedCtx := context.WithValue(ctx, toolInvocationContextKey{}, scope)

	defer func() {
		if recovered := recover(); recovered != nil {
			prepared.invocation.Release()
			prepared.err = fmt.Errorf("tool panicked: %v", recovered)
		}
	}()

	prepared.result, prepared.err = (&prepared.invocation).Execute(nestedCtx)
}

func (runtime *Runtime) finalizePreparedToolCall(
	ctx context.Context,
	prepared *preparedToolCall,
	onEvent func(StreamEvent),
) ToolEvent {
	if prepared.lifecycleRejected {
		event := toolLifecycleErrorEvent(prepared.call, prepared.err)
		emitProviderToolResult(onEvent, &event)

		return event
	}

	event := toolEventFromResult(prepared.call, prepared.result, prepared.err)
	if lifecycleErr := runtime.dispatchToolResultLifecycle(ctx, &event); lifecycleErr != nil && runtime.logger != nil {
		runtime.logger.Debug("tool result lifecycle failed", "error", lifecycleErr)
	}

	emitProviderToolResult(onEvent, &event)

	return event
}

type toolInvocationScope struct {
	onEvent      func(StreamEvent)
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
	callEvent := toolCallEvent(call)
	event, _ := runtime.invokeToolResult(ctx, registry, &callEvent, onEvent)

	return event
}

func toolCallEvent(call *ToolCall) ToolCallEvent {
	event := ToolCallEvent{
		ArgumentsJSON: "",
		ID:            "",
		ParentCallID:  "",
		Name:          "",
		Arguments:     tool.EmptyArguments(),
		Sequence:      0,
	}
	if call != nil {
		event.Arguments = call.Arguments
		event.ID = call.ID
		event.ParentCallID = stringFromOptions(call.Metadata, toolParentCallIDMetadataKey)
		event.Name = call.Name
		event.ArgumentsJSON = call.ArgumentsJSON
		event.Sequence = sequenceFromOptions(call.Metadata)
	}

	if event.ID == "" {
		event.ID = uuid.Must(uuid.NewV7()).String()
	}

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
		onEvent: onEvent, parentCallID: callEvent.ID, nextSequence: 0, mu: sync.Mutex{},
	}
	nestedCtx := context.WithValue(ctx, toolInvocationContextKey{}, scope)
	result, err := registry.Execute(nestedCtx, callEvent.Name, callEvent.Arguments)

	event := toolEventFromResult(callEvent, result, err)
	if lifecycleErr := runtime.dispatchToolResultLifecycle(ctx, &event); lifecycleErr != nil && runtime.logger != nil {
		runtime.logger.Debug("tool result lifecycle failed", "error", lifecycleErr)
	}

	emitProviderToolResult(onEvent, &event)
	result = canonicalToolResult(result, &event)

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

func canonicalToolResult(result tool.Result, event *ToolEvent) tool.Result {
	if event == nil {
		return result
	}

	if event.Result != result.Text() {
		result.Content = []tool.ContentBlock{{Type: tool.ContentTypeText, Text: event.Result, Data: "", MIMEType: ""}}
	}

	if strings.TrimSpace(event.DetailsJSON) == "" {
		result.Details = nil

		return result
	}

	result.Details = nil

	var details map[string]any
	if err := json.Unmarshal([]byte(event.DetailsJSON), &details); err == nil {
		result.Details = details
	}

	return result
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
