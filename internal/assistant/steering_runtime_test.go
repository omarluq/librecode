package assistant_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/tool"
)

const pendingCompactionOperationID = "pending"

const steeringTestMIMEPNG = "image/png"

type steeringLifecycleCompleter struct {
	entered      chan struct{}
	proceed      chan struct{}
	checkpointed chan []llm.Message
	mode         string
	enterOnce    sync.Once
}

type steeringCheckpointLifecycleCompleter struct {
	entered      chan struct{}
	proceed      chan struct{}
	checkpointed chan []llm.Message
	enterOnce    sync.Once
}

func newSteeringCheckpointLifecycleCompleter() *steeringCheckpointLifecycleCompleter {
	return &steeringCheckpointLifecycleCompleter{
		entered:      make(chan struct{}),
		proceed:      make(chan struct{}, 2),
		checkpointed: make(chan []llm.Message, 2),
		enterOnce:    sync.Once{},
	}
}

func (client *steeringCheckpointLifecycleCompleter) Complete(
	ctx context.Context,
	request *assistant.CompletionRequest,
) (*assistant.CompletionResult, error) {
	client.enterOnce.Do(func() { close(client.entered) })

	rounds := []struct {
		text  string
		usage llm.Usage
	}{
		{text: "first checkpoint", usage: steeringRoundUsage(10, 2)},
		{text: "second checkpoint", usage: steeringRoundUsage(25, 7)},
	}
	for _, round := range rounds {
		select {
		case <-client.proceed:
		case <-ctx.Done():
			return nil, oops.In("assistant_test").Code("completion_canceled").Wrapf(ctx.Err(), "wait for round")
		}

		messages, err := request.OnRoundCheckpoint(ctx, &llm.CompletedRound{
			Assistant: llm.Message{
				Metadata: nil, Role: llm.RoleAssistant, Content: []llm.Part{llm.TextPart(round.text)},
			},
			ToolResults: nil, FinishReason: llm.FinishReasonToolCalls, Usage: round.usage,
		})
		if err != nil {
			return nil, oops.In("assistant_test").Code("checkpoint").Wrapf(err, "checkpoint round")
		}

		client.checkpointed <- messages
	}

	finalUsage := steeringRoundUsage(40, 11)

	_, err := request.OnRoundCheckpoint(ctx, &llm.CompletedRound{
		Assistant: llm.Message{
			Metadata: nil, Role: llm.RoleAssistant, Content: []llm.Part{llm.TextPart("final")},
		},
		ToolResults: nil, FinishReason: llm.FinishReasonStop, Usage: finalUsage,
	})
	if err != nil {
		return nil, oops.In("assistant_test").Code("checkpoint").Wrapf(err, "checkpoint final round")
	}

	usage := model.TokenUsage{
		Provenance: "",
		Breakdown:  nil, TopContributors: nil, ContextWindow: 0, ContextTokens: 0,
		InputTokens: 40, OutputTokens: 11,
	}

	return &assistant.CompletionResult{
		Termination:  llm.NewTerminationMetadata("", "", ""),
		FinishReason: llm.FinishReasonStop,
		Text:         "final",
		Thinking:     nil,
		ToolEvents:   nil,
		Usage:        usage.WithReported(),
	}, nil
}

func steeringRoundUsage(input, output int) llm.Usage {
	usage := llm.Usage{
		Provenance: "",
		Breakdown:  nil, TopContributors: nil, ContextWindow: 0, ContextTokens: 0,
		InputTokens: input, OutputTokens: output,
	}

	return usage.WithReported()
}

func newSteeringLifecycleCompleter(mode string) *steeringLifecycleCompleter {
	return &steeringLifecycleCompleter{
		entered:      make(chan struct{}),
		proceed:      make(chan struct{}),
		checkpointed: make(chan []llm.Message, 1),
		mode:         mode,
		enterOnce:    sync.Once{},
	}
}

func (client *steeringLifecycleCompleter) Complete(
	ctx context.Context,
	request *assistant.CompletionRequest,
) (*assistant.CompletionResult, error) {
	client.enterOnce.Do(func() { close(client.entered) })

	select {
	case <-client.proceed:
	case <-ctx.Done():
		return nil, oops.In("assistant_test").Code("completion_canceled").Wrapf(ctx.Err(), "wait to complete")
	}

	if client.mode == "error" {
		return nil, errors.New(testProviderFailure)
	}

	messages, err := request.OnRoundCheckpoint(ctx, &llm.CompletedRound{
		Assistant: llm.Message{
			Metadata: nil,
			Role:     llm.RoleAssistant,
			Content:  []llm.Part{llm.TextPart("settled round")},
		},
		ToolResults:  nil,
		FinishReason: llm.FinishReasonStop,
		Usage:        llm.EmptyUsage(),
	})
	if err != nil {
		return nil, oops.In("assistant_test").Code("checkpoint").Wrapf(err, "checkpoint completed round")
	}

	client.checkpointed <- messages

	if client.mode == "cancel_after_checkpoint" {
		request.OnEvent(assistant.StreamEvent{
			ToolCallEvent: &assistant.ToolCallEvent{
				ArgumentsJSON: testToolArgsJSON,
				ID:            testToolCallID,
				ParentCallID:  "",
				Name:          testToolName,
				Arguments:     tool.EmptyArguments(),
				Sequence:      0,
			},
			ToolEvent: nil,
			Usage:     nil,
			Kind:      assistant.StreamEventToolStart,
			Text:      testToolName,
		})

		<-ctx.Done()

		return nil, oops.In("assistant_test").Code("completion_canceled").Wrapf(ctx.Err(), "finish completion")
	}

	return &assistant.CompletionResult{
		Termination:  llm.NewTerminationMetadata("", "", ""),
		FinishReason: llm.FinishReasonStop,
		Text:         recoveredResponseText,
		Thinking:     nil,
		ToolEvents:   nil,
		Usage:        model.EmptyTokenUsage(),
	}, nil
}

func TestRuntime_PromptSteeringCheckpointsRemainInsideOneTurnWithAggregateUsage(t *testing.T) {
	t.Parallel()

	client := newSteeringCheckpointLifecycleCompleter()
	runtime, _, manager := newTestRuntimeWithManager(t, client)
	loadRuntimeExtension(t, manager, `
local lc = require("librecode")
local events = {}
lc.on("turn_start", function()
  table.insert(events, "turn_start")
end)
lc.on("message_append", function(event)
  local text = event.payload.text or ""
  if text == "steer one" or text == "steer two" then
    table.insert(events, "message_append:" .. text)
  end
end)
lc.on("turn_end", function(event)
  local usage = event.payload.usage or {}
  table.insert(events, "turn_end:" .. tostring(usage.input_tokens) .. ":" .. tostring(usage.output_tokens))
end)
lc.register_command("steering_lifecycle", "steering lifecycle", function()
  return table.concat(events, "\n")
end)
`)

	runIDs := make(chan assistant.PromptUserEntryEvent, 1)
	request := newRuntimePromptRequest(t.TempDir(), "start", "steering lifecycle and usage")
	request.OnUserEntry = func(event assistant.PromptUserEntryEvent) { runIDs <- event }

	responses := make(chan *assistant.PromptResponse, 1)
	promptErrors := make(chan error, 1)

	go func() {
		response, err := runtime.Prompt(context.Background(), request)
		responses <- response

		promptErrors <- err
	}()

	run := <-runIDs

	<-client.entered

	for index, text := range []string{"steer one", "steer two"} {
		require.NoError(t, runtime.Steer(context.Background(), &assistant.SteeringRequest{
			SessionID: run.SessionID, RunID: run.EntryID, Text: text, Images: nil, HideUserPrompt: false,
		}))

		client.proceed <- struct{}{}

		checkpointMessages := <-client.checkpointed
		require.Len(t, checkpointMessages, 1)
		assert.Equal(t, text, checkpointMessages[0].Content[0].Text, "checkpoint %d", index+1)
	}

	response := <-responses

	require.NoError(t, <-promptErrors)
	require.NotNil(t, response)
	assert.Equal(t, 40, response.Usage.InputTokens)
	assert.Equal(t, 11, response.Usage.OutputTokens)
	assert.True(t, response.Usage.Reported())

	output, err := manager.ExecuteCommand(context.Background(), "steering_lifecycle", "")
	require.NoError(t, err)
	assert.Equal(t, []string{
		"turn_start",
		"message_append:steer one",
		"message_append:steer two",
		"turn_end:40:11",
	}, splitNonEmptyLines(output))
}

func splitNonEmptyLines(value string) []string {
	if value == "" {
		return nil
	}

	result := make([]string, 0)

	for line := range strings.SplitSeq(value, "\n") {
		if line != "" {
			result = append(result, line)
		}
	}

	return result
}

func TestRuntime_PromptSteeringLifecycleCleanup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		wantPrompt   error
		name         string
		mode         string
		cancel       bool
		wantReturned bool
	}{
		{
			name: "success consumes pending steering", mode: "success", wantPrompt: nil,
			cancel: false, wantReturned: false,
		},
		{
			name: "provider error returns pending steering", mode: "error",
			wantPrompt: errors.New(testProviderFailure), cancel: false, wantReturned: true,
		},
		{
			name: "cancellation returns pending steering", mode: "blocked", wantPrompt: context.Canceled,
			cancel: true, wantReturned: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			testRuntimePromptSteeringLifecycleCleanup(
				t, testCase.mode, testCase.wantPrompt, testCase.cancel, testCase.wantReturned,
			)
		})
	}
}

func testRuntimePromptSteeringLifecycleCleanup(
	t *testing.T,
	mode string,
	wantPrompt error,
	cancelPrompt bool,
	wantReturned bool,
) {
	t.Helper()

	client := newSteeringLifecycleCompleter(mode)
	runtime, _ := newTestRuntimeWithClient(t, client)

	ctx, cancel := context.WithCancel(context.Background())

	defer cancel()

	runIDs := make(chan assistant.PromptUserEntryEvent, 1)
	returned := make(chan []assistant.SteeringMessage, 2)
	request := newRuntimePromptRequest(t.TempDir(), "start", "steering lifecycle")
	request.OnUserEntry = func(event assistant.PromptUserEntryEvent) { runIDs <- event }
	request.OnSteeringReturn = func(messages []assistant.SteeringMessage) { returned <- messages }

	promptErr := make(chan error, 1)

	go func() {
		_, err := runtime.Prompt(ctx, request)
		promptErr <- err
	}()

	run := <-runIDs

	<-client.entered // Registration is complete before model execution starts.

	imageData := runtimeTestPNG(t, 1, 1)
	originalFirstByte := imageData[0]

	require.NoError(t, runtime.Steer(context.Background(), &assistant.SteeringRequest{
		SessionID: run.SessionID,
		RunID:     run.EntryID,
		Text:      "first",
		Images: []assistant.ImageAttachment{{
			Name: "first.png", MIMEType: steeringTestMIMEPNG, Data: imageData, Width: 1, Height: 1,
		}},
		HideUserPrompt: false,
	}))
	require.NoError(t, runtime.Steer(context.Background(), &assistant.SteeringRequest{
		SessionID: run.SessionID, RunID: run.EntryID, Text: "second", Images: nil, HideUserPrompt: false,
	}))

	imageData[0] ^= 0xff

	if cancelPrompt {
		cancel()
	} else {
		close(client.proceed)
	}

	err := <-promptErr
	if wantPrompt == nil {
		require.NoError(t, err)
	} else {
		require.Error(t, err)
		require.ErrorContains(t, err, wantPrompt.Error())
	}

	require.ErrorIs(t, runtime.Steer(context.Background(), &assistant.SteeringRequest{
		SessionID: run.SessionID, RunID: run.EntryID, Text: "late", Images: nil, HideUserPrompt: false,
	}), assistant.ErrSteeringInactive)

	assertSteeringDisposition(t, client, returned, wantReturned, originalFirstByte)

	select {
	case duplicate := <-returned:
		t.Fatalf("steering returned more than once: %#v", duplicate)
	default:
	}
}

func assertSteeringDisposition(
	t *testing.T,
	client *steeringLifecycleCompleter,
	returned <-chan []assistant.SteeringMessage,
	wantReturned bool,
	originalFirstByte byte,
) {
	t.Helper()

	if wantReturned {
		messages := <-returned
		require.Len(t, messages, 2)
		assert.Equal(t, []string{"first", "second"}, []string{messages[0].Text, messages[1].Text})
		require.Len(t, messages[0].Images, 1)
		assert.Equal(t, originalFirstByte, messages[0].Images[0].Data[0])

		return
	}

	messages := <-client.checkpointed
	require.Len(t, messages, 2)
	assert.Equal(t, "first", messages[0].Content[0].Text)
	assert.Equal(t, "second", messages[1].Content[0].Text)
}

func TestRuntime_PromptCancellationReturnsOnlyUnconsumedSteering(t *testing.T) {
	t.Parallel()

	client := newSteeringLifecycleCompleter("cancel_after_checkpoint")
	runtime, repository := newTestRuntimeWithClient(t, client)

	ctx, cancel := context.WithCancel(context.Background())

	defer cancel()

	runIDs := make(chan assistant.PromptUserEntryEvent, 1)
	returned := make(chan []assistant.SteeringMessage, 1)
	request := newRuntimePromptRequest(t.TempDir(), "start", "consumed steering cancellation")
	request.OnUserEntry = func(event assistant.PromptUserEntryEvent) { runIDs <- event }
	request.OnSteeringReturn = func(messages []assistant.SteeringMessage) { returned <- messages }

	promptErr := make(chan error, 1)

	go func() {
		_, err := runtime.Prompt(ctx, request)
		promptErr <- err
	}()

	run := <-runIDs

	<-client.entered
	require.NoError(t, runtime.Steer(context.Background(), &assistant.SteeringRequest{
		SessionID: run.SessionID, RunID: run.EntryID, Text: "consumed", Images: nil, HideUserPrompt: false,
	}))
	close(client.proceed)

	checkpointMessages := <-client.checkpointed
	require.Len(t, checkpointMessages, 1)
	assert.Equal(t, "consumed", checkpointMessages[0].Content[0].Text)

	require.NoError(t, runtime.Steer(context.Background(), &assistant.SteeringRequest{
		SessionID:      run.SessionID,
		RunID:          run.EntryID,
		Text:           pendingCompactionOperationID,
		Images:         nil,
		HideUserPrompt: false,
	}))
	cancel()
	require.ErrorIs(t, <-promptErr, context.Canceled)

	messages := <-returned
	require.Len(t, messages, 1)
	assert.Equal(t, pendingCompactionOperationID, messages[0].Text)

	persisted, err := repository.Messages(context.Background(), run.SessionID)
	require.NoError(t, err)
	require.Len(t, persisted, 5)
	assert.Equal(t, []database.Role{
		database.RoleUser,
		database.RoleAssistant,
		database.RoleUser,
		database.RoleToolResult,
		database.RoleCustom,
	}, []database.Role{
		persisted[0].Role, persisted[1].Role, persisted[2].Role, persisted[3].Role, persisted[4].Role,
	})
	assert.Equal(t, []string{"start", "settled round", "consumed"}, []string{
		persisted[0].Content, persisted[1].Content, persisted[2].Content,
	})
	assert.Contains(t, persisted[3].Content, testToolCanceledMessage)
	assert.Contains(t, persisted[3].Content, "is_error: true")
	assert.Equal(t, testPromptCanceledMessage, persisted[4].Content)
}
