package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/tool"
)

const (
	testCallID             = "call-1"
	testToolPath           = "README.md"
	testThinkingChunkUse   = "use "
	testThinkingChunkThe   = "the"
	testJoinedThinkingText = "use the tool"
)

func TestJoinedThinkingDeltas(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		deltas []string
		want   []string
	}{
		{name: "nil", deltas: nil, want: nil},
		{name: "empty input", deltas: []string{}, want: nil},
		{name: "whitespace only", deltas: []string{" ", "\t\n"}, want: nil},
		{name: "split words", deltas: []string{"rea", "son", "ing"}, want: []string{"reasoning"}},
		{
			name:   "boundary spaces",
			deltas: []string{" use ", testThinkingChunkThe, " tool "},
			want:   []string{testJoinedThinkingText},
		},
		{name: "newlines", deltas: []string{"\nfirst\n", "second\n"}, want: []string{"first\nsecond"}},
		{name: "unicode", deltas: []string{"  düşün", "ce 世界  "}, want: []string{"düşünce 世界"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := joinedThinkingDeltas(test.deltas)
			assert.Equal(t, test.want, got)

			if len(test.want) > 0 {
				assert.Len(t, got, 1)
			}
		})
	}
}

func TestJoinedThinkingDeltasCoalescesThousandsOfChunks(t *testing.T) {
	t.Parallel()

	const deltaCount = 10_000

	deltas := make([]string, deltaCount)
	for index := range deltas {
		deltas[index] = "x"
	}

	thinking := joinedThinkingDeltas(deltas)

	require.Len(t, thinking, 1)
	assert.Len(t, thinking[0], deltaCount)
}

func TestValidateToolDispatchRejectsLengthTruncatedCalls(t *testing.T) {
	t.Parallel()

	err := validateToolDispatch(llm.FinishReasonLength, []ToolCall{{
		Arguments: tool.EmptyArguments(), Metadata: nil, ID: testCallID,
		Name: "bash", ArgumentsJSON: `{"background":{"arguments":{"command":"go test ./..."}}}`,
	}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing possibly partial invocations")
}

func TestValidateToolCallsRejectsMissingFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		call ToolCall
	}{
		{
			name: "missing id",
			call: ToolCall{
				Arguments:     tool.EmptyArguments(),
				Metadata:      nil,
				ID:            "",
				Name:          jsonReadToolName,
				ArgumentsJSON: "",
			},
		},
		{
			name: "missing name",
			call: ToolCall{
				Arguments:     tool.EmptyArguments(),
				Metadata:      nil,
				ID:            testCallID,
				Name:          "",
				ArgumentsJSON: "",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateToolCalls([]ToolCall{tt.call})
			require.Error(t, err)
		})
	}
}

func TestExecuteToolCallsRequiresExecutor(t *testing.T) {
	t.Parallel()

	outputs, events, err := executeToolCalls(
		context.Background(),
		emptyCompletionRequest(),
		[]ToolCall{readToolCall(testCallID)},
	)

	require.Error(t, err)
	assert.Nil(t, outputs)
	assert.Nil(t, events)
}

func TestExecuteToolCallsWrapsExecutorErrors(t *testing.T) {
	t.Parallel()

	request := emptyCompletionRequest()
	request.ExecuteTools = func(
		_ context.Context,
		_ []llm.ToolCall,
		_ func(*llm.StreamChunk),
	) ([]llm.ToolResult, error) {
		return nil, errors.New("boom")
	}

	outputs, events, err := executeToolCalls(
		context.Background(),
		request,
		[]ToolCall{readToolCall(testCallID)},
	)

	require.Error(t, err)
	assert.Nil(t, outputs)
	assert.Nil(t, events)

	var coded oops.OopsError
	require.ErrorAs(t, err, &coded)
	assert.Equal(t, "tool_execution_failed", coded.Code())
	assert.Equal(t, "provider", coded.Domain())
}

func TestExecuteToolCallsUsesInjectedExecutorAndHandlesMissingEvents(t *testing.T) {
	t.Parallel()

	request := emptyCompletionRequest()
	request.ExecuteTools = func(
		_ context.Context,
		calls []llm.ToolCall,
		_ func(*llm.StreamChunk),
	) ([]llm.ToolResult, error) {
		require.Len(t, calls, 1)
		assert.Equal(t, testCallID, calls[0].ID)

		return []llm.ToolResult{}, nil
	}

	outputs, events, err := executeToolCalls(
		context.Background(),
		request,
		[]ToolCall{readToolCall(testCallID)},
	)

	require.NoError(t, err)
	assert.Empty(t, events)
	require.Len(t, outputs, 1)
	assert.Equal(t, map[string]any{
		jsonTypeKey:   functionCallOutputType,
		jsonCallIDKey: testCallID,
		jsonOutputKey: "",
	}, outputs[0])
}

func TestToolCallMetadataClonesMetadata(t *testing.T) {
	t.Parallel()

	call := readToolCall(testCallID)
	call.Metadata = map[string]any{testExistingKey: true}
	metadata := toolCallMetadata(&call)
	metadata[testExistingKey] = false

	assertIsTrue(t, call.Metadata[testExistingKey])
}

func TestToolOutputTextIncludesDetailsForEmptyResult(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "details:\n{}", toolOutputText("   ", "{}"))
	assert.Equal(t, "plain", toolOutputText("plain", ""))
	assert.Equal(t, "plain\ndetails:\n{}", toolOutputText(" plain ", "{}"))
}

func TestEncodeToolDetailsReturnsEmptyForInvalidDetails(t *testing.T) {
	t.Parallel()

	encoded := encodeToolDetails(map[string]any{"bad": func() {}})
	assert.Empty(t, encoded)
}

func TestOpenAIChatToolMessagesRejectsMismatchedCallsAndEvents(t *testing.T) {
	t.Parallel()

	messages, err := openAIChatToolMessages([]ToolCall{readToolCall("call_1")}, nil)

	require.Error(t, err)
	assert.Nil(t, messages)
	assert.Contains(t, err.Error(), "mismatched tool calls and results")
}

func TestOpenAIChatToolMessagesUsesCallIDs(t *testing.T) {
	t.Parallel()

	messages, err := openAIChatToolMessages(
		[]ToolCall{readToolCall("call_1")},
		[]ToolEvent{{
			Name:          jsonReadToolName,
			ArgumentsJSON: `{}`,
			DetailsJSON:   "",
			Result:        "ok",
			Error:         "",
			IsError:       false,
		}},
	)

	require.NoError(t, err)
	require.Len(t, messages, 1)
	assert.JSONEq(t, jsonString(jsonToolRole), jsonString(messages[0][jsonRoleKey]))
	assert.Equal(t, "call_1", messages[0]["tool_call_id"])
	assert.Equal(t, "ok", messages[0][jsonContentKey])
}

func readToolCall(callID string) ToolCall {
	return ToolCall{
		Arguments:     tool.EmptyArguments(),
		Metadata:      nil,
		ID:            callID,
		Name:          jsonReadToolName,
		ArgumentsJSON: `{}`,
	}
}
