package assistant_test

import (
	"context"
	"errors"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/model"
)

const (
	autoCompactionTestFinalAnswer     = "final answer"
	autoCompactionTestUnused          = "unused"
	testContextWindowExceededOopsCode = "context_window_exceeded"
)

type recordingCompleter struct {
	complete           func(call int, request *assistant.CompletionRequest) (*assistant.CompletionResult, error)
	requests           []*assistant.CompletionRequest
	disableToolsByCall []bool
}

func (client *recordingCompleter) Complete(
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

func newSequencedCompleter(responses ...string) *recordingCompleter {
	return &recordingCompleter{
		complete: func(call int, request *assistant.CompletionRequest) (*assistant.CompletionResult, error) {
			response := "ok"
			if len(responses) >= call {
				response = responses[call-1]
			}

			if request.DisableTools {
				response = structuredTestSummary(response)
			}

			return testCompletionResult(response), nil
		},
		requests:           nil,
		disableToolsByCall: nil,
	}
}

func newSummaryAwareCompleter(
	summary string,
	summaryErr error,
	final string,
) *recordingCompleter {
	return &recordingCompleter{
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

func newOverflowRecoveryCompleter(
	summary string,
	final string,
	overflowErr error,
) *recordingCompleter {
	return &recordingCompleter{
		complete: func(call int, request *assistant.CompletionRequest) (*assistant.CompletionResult, error) {
			if request.DisableTools {
				return testCompletionResult(structuredTestSummary(summary)), nil
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

func newOverflowSummaryCompleter(summary string, summaryErr error) *recordingCompleter {
	return &recordingCompleter{
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
	return oops.In("assistant").Code(testContextWindowExceededOopsCode).Errorf("provider context window exceeded")
}

func structuredTestSummary(text string) string {
	return "## Goal\n- " + text +
		"\n## User constraints and preferences\n- x" +
		"\n## Completed work\n- x\n## Work in progress\n- x" +
		"\n## Files changed/read\n- x\n## Commands and validation\n- x" +
		"\n## Decisions\n- x\n## Errors and blockers\n- x" +
		"\n## Exact next steps\n- x"
}

func testCompletionResult(text string) *assistant.CompletionResult {
	return &assistant.CompletionResult{
		Termination:  llm.NewTerminationMetadata("", "", ""),
		FinishReason: llm.FinishReasonStop,
		Text:         text,
		Thinking:     nil,
		ToolEvents:   nil,
		Usage:        model.EmptyTokenUsage(),
	}
}

func failingSummaryClient() *recordingCompleter {
	return newSummaryAwareCompleter("", errors.New("summary failed"), autoCompactionTestFinalAnswer)
}

func largeSummaryClient(words int) *recordingCompleter {
	return newSummaryAwareCompleter(strings.Repeat("summary ", words), nil, autoCompactionTestFinalAnswer)
}
