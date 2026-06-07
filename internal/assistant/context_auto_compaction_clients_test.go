package assistant_test

import (
	"context"
	"errors"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/model"
)

const (
	autoCompactionTestFinalAnswer     = "final answer"
	autoCompactionTestUnused          = "unused"
	testContextWindowExceededOopsCode = "context_window_exceeded"
)

type recordingCompletionClient struct {
	complete           func(call int, request *assistant.CompletionRequest) (*assistant.CompletionResult, error)
	requests           []*assistant.CompletionRequest
	disableToolsByCall []bool
}

func (client *recordingCompletionClient) Complete(
	_ context.Context,
	request *assistant.CompletionRequest,
) (*assistant.CompletionResult, error) {
	client.requests = append(client.requests, request)
	client.disableToolsByCall = append(client.disableToolsByCall, request.DisableTools)
	if client.complete == nil {
		return testCompletionResult("ok"), nil
	}

	return client.complete(len(client.requests), request)
}

func newSequencedCompletionClient(responses ...string) *recordingCompletionClient {
	return &recordingCompletionClient{
		complete: func(call int, _ *assistant.CompletionRequest) (*assistant.CompletionResult, error) {
			response := "ok"
			if len(responses) >= call {
				response = responses[call-1]
			}

			return testCompletionResult(response), nil
		},
		requests:           nil,
		disableToolsByCall: nil,
	}
}

func newSummaryAwareCompletionClient(
	summary string,
	summaryErr error,
	final string,
) *recordingCompletionClient {
	return &recordingCompletionClient{
		complete: func(_ int, request *assistant.CompletionRequest) (*assistant.CompletionResult, error) {
			if request.DisableTools {
				if summaryErr != nil {
					return nil, summaryErr
				}

				return testCompletionResult(summary), nil
			}

			return testCompletionResult(final), nil
		},
		requests:           nil,
		disableToolsByCall: nil,
	}
}

func newOverflowRecoveryCompletionClient(
	summary string,
	final string,
	overflowErr error,
) *recordingCompletionClient {
	return &recordingCompletionClient{
		complete: func(call int, request *assistant.CompletionRequest) (*assistant.CompletionResult, error) {
			if request.DisableTools {
				return testCompletionResult(summary), nil
			}
			switch call {
			case 1:
				if overflowErr != nil {
					return nil, overflowErr
				}

				return nil, testContextWindowError()
			case 3:
				if final == autoCompactionTestUnused {
					return nil, testContextWindowError()
				}
			}

			return testCompletionResult(final), nil
		},
		requests:           nil,
		disableToolsByCall: nil,
	}
}

func newOverflowSummaryCompletionClient(summary string, summaryErr error) *recordingCompletionClient {
	return &recordingCompletionClient{
		complete: func(_ int, request *assistant.CompletionRequest) (*assistant.CompletionResult, error) {
			if request.DisableTools {
				if summaryErr != nil {
					return nil, summaryErr
				}

				return testCompletionResult(summary), nil
			}

			return nil, testContextWindowError()
		},
		requests:           nil,
		disableToolsByCall: nil,
	}
}

func testContextWindowError() error {
	return oops.In("assistant").Code("responses_status").Errorf("maximum context length exceeded")
}

func testCompletionResult(text string) *assistant.CompletionResult {
	return &assistant.CompletionResult{
		Text:       text,
		Thinking:   nil,
		ToolEvents: nil,
		Usage:      model.EmptyTokenUsage(),
	}
}

func failingSummaryClient() *recordingCompletionClient {
	return newSummaryAwareCompletionClient("", errors.New("summary failed"), autoCompactionTestFinalAnswer)
}

func largeSummaryClient(words int) *recordingCompletionClient {
	return newSummaryAwareCompletionClient(strings.Repeat("summary ", words), nil, autoCompactionTestFinalAnswer)
}
