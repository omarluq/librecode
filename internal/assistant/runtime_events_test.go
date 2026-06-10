package assistant_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/samber/ro"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/event"
	"github.com/omarluq/librecode/internal/model"
)

const (
	testCompletionText = "done"
	testToolName       = "read"
	testToolPath       = "README.md"
	testToolPathKey    = "path"
	testToolArgsJSON   = `{"path":"README.md"}`
	testToolCallID     = "call-1"
	testToolResult     = "contents"
)

func TestRuntime_ProviderLifecyclePublishesReactiveEvents(t *testing.T) {
	t.Parallel()

	runtime, _, _ := newTestRuntimeWithManager(t, staticCompleter{
		result: &assistant.CompletionResult{
			Text:       testCompletionText,
			Thinking:   nil,
			ToolEvents: nil,
			Usage: model.TokenUsage{
				Breakdown:       nil,
				TopContributors: nil,
				ContextWindow:   100,
				ContextTokens:   9,
				InputTokens:     9,
				OutputTokens:    3,
			},
		},
		err: nil,
	})
	events := collectRuntimeChannels(t, runtime.EventBus())

	_, err := runtime.Prompt(context.Background(), newRuntimePromptRequest(testRuntimeCWD, "provider", ""))
	require.NoError(t, err)

	assert.Contains(t, *events, "before_provider_request")
	assert.Contains(t, *events, "after_provider_response")
}

func TestRuntime_ProviderErrorPublishesReactiveEvent(t *testing.T) {
	t.Parallel()

	runtime, _, _ := newTestRuntimeWithManager(t, staticCompleter{
		result: nil,
		err:    errors.New("provider unavailable"),
	})
	events := collectRuntimeChannels(t, runtime.EventBus())

	_, err := runtime.Prompt(context.Background(), newRuntimePromptRequest(testRuntimeCWD, "provider", ""))
	require.Error(t, err)

	assert.Contains(t, *events, "before_provider_request")
	assert.Contains(t, *events, "provider_error")
}

func TestRuntime_ToolCallbacksPublishReactiveEvents(t *testing.T) {
	t.Parallel()

	client := &toolCallbackClient{}
	runtime, _, _ := newTestRuntimeWithManager(t, client)
	events := collectRuntimeChannels(t, runtime.EventBus())
	request := newRuntimePromptRequest(testRuntimeCWD, "tool callbacks", "")

	_, err := runtime.Prompt(context.Background(), request)
	require.NoError(t, err)
	assert.Contains(t, strings.Join(*events, ","), "tool_call")
	assert.Contains(t, strings.Join(*events, ","), "tool_result")
}

func collectRuntimeChannels(t *testing.T, bus *event.Bus) *[]string {
	t.Helper()

	channels := []string{}
	subscription := bus.Stream().Subscribe(ro.NewObserver(
		func(envelope event.Envelope) {
			channels = append(channels, envelope.Channel)
		},
		func(error) {},
		func() {},
	))
	t.Cleanup(subscription.Unsubscribe)

	return &channels
}

type toolCallbackClient struct{}

func (toolCallbackClient) Complete(
	ctx context.Context,
	request *assistant.CompletionRequest,
) (*assistant.CompletionResult, error) {
	if request.ExecuteTools != nil {
		_, err := request.ExecuteTools(ctx, []assistant.ToolCall{{
			Metadata:      nil,
			Arguments:     map[string]any{testToolPathKey: testToolPath},
			ID:            testToolCallID,
			Name:          testToolName,
			ArgumentsJSON: testToolArgsJSON,
		}}, request.OnEvent)
		if err != nil {
			return nil, err
		}
	}

	return &assistant.CompletionResult{
		Text:       testCompletionText,
		Thinking:   nil,
		ToolEvents: nil,
		Usage:      model.EmptyTokenUsage(),
	}, nil
}
