package assistant

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/contextwindow"
	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/model"
)

const (
	adapterSessionID     = "session-1"
	adapterThinkingLevel = "high"
	adapterCWD           = "/work"
	adapterHeaderKey     = "x-test"
	adapterBaseURL       = "https://example.test"
	adapterHeaderValue   = "enabled"
	adapterThought       = " thought "
	adapterBlankThought  = "   "
	adapterReadPath      = "README.md"
	adapterReadArgs      = `{"path":"README.md"}`
	adapterBashArgs      = `{"command":"false"}`
	adapterHello         = "hello"
)

func TestProviderRequestFromCompletionRequestAdaptsCallbacksAndRequest(t *testing.T) {
	t.Parallel()

	observed := false
	request := &CompletionRequest{
		OnEvent:           nil,
		OnProviderObserve: providerObserveAssertion(t, &observed),
		OnProviderRequest: nil,
		ToolRegistry:      nil,
		ExecuteTools:      nil,
		SessionID:         adapterSessionID,
		SystemPrompt:      jsonSystemRole,
		ThinkingLevel:     adapterThinkingLevel,
		CWD:               adapterCWD,
		Auth: model.RequestAuth{
			Headers: map[string]string{adapterHeaderKey: adapterHeaderValue},
			APIKey:  "secret",
			Error:   "",
			OK:      true,
		},
		Messages: nil,
		Usage:    model.EmptyTokenUsage(),
		Model: model.Model{
			ThinkingLevelMap: map[model.ThinkingLevel]*string{model.ThinkingHigh: new("enabled")},
			Headers:          nil,
			Compat:           map[string]any{"feature": "on"},
			Provider:         "anthropic",
			ID:               "claude",
			Name:             "Claude",
			API:              apiAnthropicMessages,
			BaseURL:          adapterBaseURL,
			Input:            nil,
			Cost:             model.Cost{Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0},
			ContextWindow:    200,
			MaxTokens:        50,
			Reasoning:        true,
		},
		ProviderAttempt: 3,
		DisableTools:    false,
	}

	converted := providerRequestFromCompletionRequest(request)
	require.NotNil(t, converted)
	assert.Equal(t, adapterSessionID, converted.Request.SessionID)
	assert.Equal(t, "claude", converted.Request.Model.ID)
	assert.Equal(t, adapterThinkingLevel, converted.Request.ThinkingLevel)
	assert.Equal(t, 3, converted.ProviderAttempt)
	require.NotNil(t, converted.OnProviderObserve)

	converted.OnProviderObserve(context.Background(), adapterHookInput())
	assert.True(t, observed)

	empty := providerRequestFromCompletionRequest(nil)
	require.NotNil(t, empty)
	assert.Empty(t, empty.Request.SessionID)
	assert.Equal(t, 0, empty.ProviderAttempt)
	assert.Nil(t, empty.OnProviderObserve)
}

func TestCompletionResultFromLLMResponseConvertsPartsAndUsage(t *testing.T) {
	t.Parallel()

	response := &llm.Response{
		FinishReason: llm.FinishReasonStop,
		Content: []llm.Part{
			llm.TextPart(" first "),
			llm.TextPart("second"),
			reasoningPart(adapterThought),
			reasoningPart(adapterBlankThought),
			toolResultPart(&llm.ToolResult{
				Metadata:      map[string]any{"details_json": `{"ok":true}`},
				ToolCallID:    "",
				ArgumentsJSON: adapterReadArgs,
				Name:          jsonReadToolName,
				Error:         "",
				Content:       []llm.Part{llm.TextPart("tool output")},
				IsError:       false,
			}),
			toolResultPart(&llm.ToolResult{
				Metadata:      nil,
				ToolCallID:    "",
				ArgumentsJSON: adapterBashArgs,
				Name:          jsonBashToolName,
				Error:         "failed",
				Content:       []llm.Part{llm.TextPart("exit status 1")},
				IsError:       true,
			}),
		},
		ToolCalls: nil,
		Usage: llm.Usage{
			Breakdown:       map[string]int{contextwindow.BreakdownHistory: 7},
			TopContributors: nil,
			ContextWindow:   100,
			ContextTokens:   20,
			InputTokens:     15,
			OutputTokens:    5,
		},
	}

	converted := completionResultFromLLMResponse(response)
	require.NotNil(t, converted)
	assert.Equal(t, llm.FinishReasonStop, converted.FinishReason)
	assert.Equal(t, "first \nsecond", converted.Text)
	assert.Equal(t, []string{adapterThought}, converted.Thinking)
	require.Len(t, converted.ToolEvents, 2)
	assert.Equal(t, expectedReadToolName, converted.ToolEvents[0].Name)
	assert.Equal(t, `{"ok":true}`, converted.ToolEvents[0].DetailsJSON)
	assert.False(t, converted.ToolEvents[0].IsError)
	assert.Equal(t, "failed", converted.ToolEvents[1].Error)
	assert.True(t, converted.ToolEvents[1].IsError)
	assert.Equal(t, 20, converted.Usage.ContextTokens)

	empty := completionResultFromLLMResponse(nil)
	require.NotNil(t, empty)
	assert.Equal(t, llm.FinishReasonUnknown, empty.FinishReason)
	assert.Empty(t, empty.Text)
	assert.Empty(t, empty.Thinking)
	assert.Empty(t, empty.ToolEvents)
}

func TestStreamEventAdaptersRoundTrip(t *testing.T) {
	t.Parallel()

	usage := model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   100,
		ContextTokens:   25,
		InputTokens:     20,
		OutputTokens:    5,
	}
	cases := []struct {
		name     string
		event    StreamEvent
		wantPart llm.PartType
		wantKind StreamEventKind
	}{
		{
			name:     "text",
			event:    streamEvent(StreamEventTextDelta, adapterHello, nil, &usage),
			wantPart: llm.PartText,
			wantKind: StreamEventTextDelta,
		},
		{
			name:     "thinking",
			event:    streamEvent(StreamEventThinkingDelta, "thought", nil, nil),
			wantPart: llm.PartReasoning,
			wantKind: StreamEventThinkingDelta,
		},
		{
			name:     "tool result",
			event:    streamEvent(StreamEventToolResult, "", adapterToolEvent(), nil),
			wantPart: llm.PartToolResult,
			wantKind: StreamEventToolResult,
		},
		{
			name:     "tool start",
			event:    streamEvent(StreamEventToolStart, jsonReadToolName, nil, nil),
			wantPart: llm.PartToolCall,
			wantKind: StreamEventToolStart,
		},
		{
			name:     "unknown text",
			event:    streamEvent(StreamEventUnknown, "?", nil, nil),
			wantPart: llm.PartText,
			wantKind: StreamEventTextDelta,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			chunk := llmStreamChunkFromEvent(testCase.event)
			require.NotNil(t, chunk)
			require.NotNil(t, chunk.Part)
			assert.Equal(t, testCase.wantPart, chunk.Part.Type)

			roundTripped := streamEventFromLLMChunk(chunk)
			assert.Equal(t, testCase.wantKind, roundTripped.Kind)
			assert.Equal(t, testCase.event.Text, roundTripped.Text)
		})
	}

	assert.Equal(t, StreamEventTextDelta, streamEventFromLLMChunk(nil).Kind)
	assert.Nil(t, streamEventFromLLMChunk(&llm.StreamChunk{
		Part:         nil,
		ToolCall:     nil,
		FinishReason: llm.FinishReasonUnknown,
		Usage:        llm.EmptyUsage(),
	}).Usage)
	assert.Nil(t, llmPartFromStreamEvent(streamEvent(StreamEventToolResult, "", nil, nil)))
}

func TestToolExecutorAdapterConvertsCallsEventsAndErrors(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("tool failed")
	errorExecutor := llmToolExecutor(func(context.Context, []ToolCall, func(StreamEvent)) ([]ToolEvent, error) {
		return nil, expectedErr
	})
	_, err := errorExecutor(context.Background(), nil, nil)
	require.ErrorIs(t, err, expectedErr)

	var (
		observedCall  ToolCall
		observedEvent *llm.StreamChunk
	)

	executor := llmToolExecutor(
		func(_ context.Context, calls []ToolCall, onEvent func(StreamEvent)) ([]ToolEvent, error) {
			require.Len(t, calls, 1)
			observedCall = calls[0]

			onEvent(streamEvent(StreamEventToolResult, "", adapterToolEvent(), nil))

			return []ToolEvent{{
				Name:          jsonReadToolName,
				ArgumentsJSON: adapterReadArgs,
				DetailsJSON:   "",
				Result:        "ok",
				Error:         "",
				IsError:       false,
			}}, nil
		},
	)

	results, err := executor(
		context.Background(),
		[]llm.ToolCall{{
			Metadata:      map[string]any{"m": true},
			Arguments:     testToolArguments(map[string]any{jsonPathKey: adapterReadPath}),
			ID:            "call-1",
			Name:          jsonReadToolName,
			ArgumentsJSON: adapterReadArgs,
		}},
		func(chunk *llm.StreamChunk) { observedEvent = chunk },
	)
	require.NoError(t, err)
	assert.Equal(t, "call-1", observedCall.ID)
	assert.Equal(t, adapterReadPath, testToolArgumentFields(observedCall.Arguments)[jsonPathKey])
	require.NotNil(t, observedEvent)
	require.NotNil(t, observedEvent.Part)
	assert.Equal(t, llm.PartToolResult, observedEvent.Part.Type)
	require.Len(t, results, 1)
	assert.Equal(t, expectedReadToolName, results[0].Name)

	assert.Nil(t, llmToolExecutor(nil))
	assert.Nil(t, assistantStreamEventHandler(nil))
	assert.Nil(t, llmStreamEventHandler(nil))
	assert.Nil(t, assistantToolCallsFromLLM(nil))
}

func TestAdapterNilAndFallbackHelpers(t *testing.T) {
	t.Parallel()

	assert.Nil(t, completionRequestFromHookInput(nil))
	assert.Equal(t, 0, hookAttempt(nil))
	assert.Empty(t, stringFromOptions(map[string]any{"value": 1}, "value"))
	assert.Empty(t, textFromLLMChunk(nil))
	assert.Nil(t, chunkPart(nil))
	assert.Equal(t, llm.EmptyUsage(), chunkUsage(nil))
	assert.Nil(t, toolEventPointerFromLLMPart(nil))
	assert.Nil(t, usagePointerFromLLMUsage(llm.EmptyUsage()))

	modelRef := modelFromLLMRef(nil)
	assert.Empty(t, modelRef.ID)
	assert.Nil(t, thinkingLevelMapFromLLM(nil))
}

func providerObserveAssertion(t *testing.T, observed *bool) func(context.Context, *CompletionRequest, int) {
	t.Helper()

	return func(_ context.Context, observedRequest *CompletionRequest, attempt int) {
		*observed = true

		assert.Equal(t, adapterSessionID, observedRequest.SessionID)
		assert.Equal(t, "claude", observedRequest.Model.ID)
		assert.Equal(t, adapterCWD, observedRequest.CWD)
		assert.Equal(t, 3, attempt)
	}
}

func adapterHookInput() *llm.HookInput {
	return &llm.HookInput{
		Payload:         nil,
		Headers:         map[string]string{adapterHeaderKey: adapterHeaderValue},
		ProviderOptions: map[string]any{"cwd": adapterCWD},
		Model: llm.ModelRef{
			Metadata:         map[string]any{"feature": "on"},
			ThinkingLevelMap: map[string]*string{adapterThinkingLevel: new("enabled")},
			Provider:         "anthropic",
			ID:               "claude",
			API:              apiAnthropicMessages,
			BaseURL:          adapterBaseURL,
			MaxTokens:        50,
			ContextWindow:    200,
			Reasoning:        true,
		},
		SessionID:     adapterSessionID,
		ThinkingLevel: adapterThinkingLevel,
		Attempt:       3,
	}
}

func reasoningPart(text string) llm.Part {
	return llm.Part{
		Metadata:   nil,
		ToolCall:   nil,
		ToolResult: nil,
		Type:       llm.PartReasoning,
		Text:       text,
		Data:       "",
		MIMEType:   "",
	}
}

func toolResultPart(result *llm.ToolResult) llm.Part {
	return llm.Part{
		Metadata:   nil,
		ToolCall:   nil,
		ToolResult: result,
		Type:       llm.PartToolResult,
		Text:       "",
		Data:       "",
		MIMEType:   "",
	}
}

func streamEvent(kind StreamEventKind, text string, event *ToolEvent, usage *model.TokenUsage) StreamEvent {
	return StreamEvent{ToolCallEvent: nil, ToolEvent: event, Usage: usage, Kind: kind, Text: text}
}

func adapterToolEvent() *ToolEvent {
	return &ToolEvent{
		Name:          jsonReadToolName,
		ArgumentsJSON: "",
		DetailsJSON:   "",
		Result:        "ok",
		Error:         "",
		IsError:       false,
	}
}
