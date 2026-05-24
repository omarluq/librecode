package extension

import (
	"context"
	"errors"
	"fmt"
	"time"

	lua "github.com/yuin/gopher-lua"
)

// LifecycleEventName identifies one agent/runtime lifecycle event.
type LifecycleEventName string

const (
	// LifecycleSessionStart fires when a new session is created.
	LifecycleSessionStart LifecycleEventName = "session_start"
	// LifecycleSessionLoad fires when an existing session is loaded.
	LifecycleSessionLoad LifecycleEventName = "session_load"
	// LifecycleSessionSave fires after durable session state is written.
	LifecycleSessionSave LifecycleEventName = "session_save"
	// LifecycleSessionShutdown fires before a session/runtime shuts down.
	LifecycleSessionShutdown LifecycleEventName = "session_shutdown"
	// LifecycleResourcesDiscover fires while project resources are discovered.
	LifecycleResourcesDiscover LifecycleEventName = "resources_discover"
	// LifecycleInput fires when raw user input enters the assistant runtime.
	LifecycleInput LifecycleEventName = "input"
	// LifecyclePromptPrepare fires before a prompt becomes a model turn.
	LifecyclePromptPrepare LifecycleEventName = "prompt_prepare"
	// LifecycleBeforeAgentStart fires before assistant turn execution starts.
	LifecycleBeforeAgentStart LifecycleEventName = "before_agent_start"
	// LifecycleAgentStart fires when assistant turn execution starts.
	LifecycleAgentStart LifecycleEventName = "agent_start"
	// LifecycleTurnStart fires when a model-facing turn starts.
	LifecycleTurnStart LifecycleEventName = "turn_start"
	// LifecycleContextBuild fires while model context is assembled.
	LifecycleContextBuild LifecycleEventName = "context_build"
	// LifecycleBeforeProviderRequest fires before a provider request is sent.
	LifecycleBeforeProviderRequest LifecycleEventName = "before_provider_request"
	// LifecycleAfterProviderResponse fires after a provider response is received.
	LifecycleAfterProviderResponse LifecycleEventName = "after_provider_response"
	// LifecycleProviderError fires when a provider request fails.
	LifecycleProviderError LifecycleEventName = "provider_error"
	// LifecycleToolCall fires before a tool call executes.
	LifecycleToolCall LifecycleEventName = "tool_call"
	// LifecycleToolResult fires after a tool call returns.
	LifecycleToolResult LifecycleEventName = "tool_result"
	// LifecycleToolError fires when a tool call fails.
	LifecycleToolError LifecycleEventName = "tool_error"
	// LifecycleMessageAppend fires after a durable message is appended.
	LifecycleMessageAppend LifecycleEventName = "message_append"
	// LifecycleTurnEnd fires when a model-facing turn ends.
	LifecycleTurnEnd LifecycleEventName = "turn_end"
	// LifecycleAgentEnd fires when assistant turn execution ends.
	LifecycleAgentEnd LifecycleEventName = "agent_end"
	// LifecycleShutdown fires before the runtime exits.
	LifecycleShutdown LifecycleEventName = "shutdown"
)

// LifecycleEvent is the runtime-neutral payload passed through lifecycle handlers.
type LifecycleEvent struct {
	Payload map[string]any     `json:"payload"`
	Name    LifecycleEventName `json:"name"`
}

// LifecycleDispatchResult describes the outcome of lifecycle handler dispatch.
type LifecycleDispatchResult struct {
	Payload         map[string]any          `json:"payload"`
	ProviderRequest ProviderRequestMutation `json:"provider_request"`
	Name            string                  `json:"name"`
	Errors          []string                `json:"errors"`
	Duration        time.Duration           `json:"duration"`
	HandlerCount    int                     `json:"handler_count"`
	Consumed        bool                    `json:"consumed"`
	Stopped         bool                    `json:"stopped"`
}

// ProviderRequestMutation contains conservative provider request changes returned by lifecycle handlers.
type ProviderRequestMutation struct {
	Headers map[string]string `json:"headers"`
}

// LifecycleDispatcher emits runtime-neutral lifecycle events.
type LifecycleDispatcher interface {
	DispatchLifecycle(ctx context.Context, event LifecycleEvent) (LifecycleDispatchResult, error)
}

// DispatchLifecycle runs registered handlers for a lifecycle event.
func (manager *Manager) DispatchLifecycle(ctx context.Context, event LifecycleEvent) (LifecycleDispatchResult, error) {
	if event.Name == "" {
		return LifecycleDispatchResult{}, fmt.Errorf("extension: lifecycle event name is required")
	}

	result := LifecycleDispatchResult{
		Payload:         cloneMap(event.Payload),
		ProviderRequest: ProviderRequestMutation{Headers: map[string]string{}},
		Name:            string(event.Name),
		Errors:          []string{},
		Duration:        0,
		HandlerCount:    0,
		Consumed:        false,
		Stopped:         false,
	}
	startedAt := time.Now()

	for _, handler := range manager.handlersFor(string(event.Name)) {
		if err := ctx.Err(); err != nil {
			result.Duration = time.Since(startedAt)
			return result, err
		}

		result.HandlerCount++
		luaResult, err := callLuaPrepared(
			handler.extension,
			nil,
			handler.function,
			func(state *lua.LState) []lua.LValue {
				return []lua.LValue{lifecycleEventTable(state, event.Name, result.Payload)}
			},
		)
		if err != nil {
			result.Errors = append(result.Errors, err.Error())
			continue
		}
		applyLifecycleLuaResult(&result, luaResult)
		if result.Stopped {
			break
		}
	}

	result.Duration = time.Since(startedAt)
	if len(result.Errors) > 0 {
		return result, errors.New("one or more lifecycle handlers failed")
	}

	return result, nil
}

func lifecycleEventTable(state *lua.LState, name LifecycleEventName, payload map[string]any) *lua.LTable {
	return mapToLuaTable(state, map[string]any{
		"name":    string(name),
		"payload": payload,
	})
}

func applyLifecycleLuaResult(result *LifecycleDispatchResult, value lua.LValue) {
	table, ok := value.(*lua.LTable)
	if !ok {
		return
	}
	if luaTableBool(table, "handled") || luaTableBool(table, "consumed") {
		result.Consumed = true
	}
	if luaTableBool(table, "stop") || luaTableBool(table, "stopped") {
		result.Consumed = true
		result.Stopped = true
	}
	payloadValue := table.RawGetString("payload")
	if payload, ok := luaValueToGo(payloadValue).(map[string]any); ok {
		result.Payload = payload
	}
	providerRequestValue := table.RawGetString("provider_request")
	if providerRequest, ok := providerRequestMutationFromLua(providerRequestValue); ok {
		result.ProviderRequest = mergeProviderRequestMutation(result.ProviderRequest, providerRequest)
	}
}

func mergeProviderRequestMutation(base, override ProviderRequestMutation) ProviderRequestMutation {
	merged := ProviderRequestMutation{Headers: map[string]string{}}
	for key, value := range base.Headers {
		merged.Headers[key] = value
	}
	for key, value := range override.Headers {
		merged.Headers[key] = value
	}

	return merged
}

func providerRequestMutationFromLua(value lua.LValue) (ProviderRequestMutation, bool) {
	payload, ok := luaValueToGo(value).(map[string]any)
	if !ok {
		return ProviderRequestMutation{Headers: map[string]string{}}, false
	}
	headers := stringMapValue(payload["headers"])
	if len(headers) == 0 {
		return ProviderRequestMutation{Headers: map[string]string{}}, false
	}

	return ProviderRequestMutation{Headers: headers}, true
}

func stringMapValue(value any) map[string]string {
	object, ok := value.(map[string]any)
	if !ok {
		return map[string]string{}
	}
	output := make(map[string]string, len(object))
	for key, item := range object {
		text, ok := item.(string)
		if !ok {
			continue
		}
		output[key] = text
	}

	return output
}
