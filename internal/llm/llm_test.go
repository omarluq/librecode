package llm_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/llm"
)

func TestBoundaryInterfaces(t *testing.T) {
	t.Parallel()

	var generator llm.Generator = testGenerator{}
	request := llm.Request{
		ProviderOptions: nil,
		Auth: llm.Auth{
			Headers: nil,
			APIKey:  "",
		},
		SystemPrompt:  "",
		ThinkingLevel: "",
		SessionID:     "",
		Messages:      []llm.Message{},
		Tools:         nil,
		Model: llm.ModelRef{
			Metadata:         nil,
			ThinkingLevelMap: nil,
			Provider:         "test",
			ID:               "model",
			API:              "",
			BaseURL:          "",
			MaxTokens:        0,
			ContextWindow:    0,
			Reasoning:        false,
		},
		Usage:        llm.EmptyUsage(),
		DisableTools: false,
	}
	response, err := generator.Generate(context.Background(), &request)
	require.NoError(t, err)
	assert.Equal(t, llm.FinishReasonStop, response.FinishReason)

	var streamer llm.Streamer = testStreamer{}
	stream, err := streamer.Stream(context.Background(), llm.EmptyRequest())
	require.NoError(t, err)
	chunk, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, llm.FinishReasonToolCalls, chunk.FinishReason)
	require.NoError(t, stream.Close())
	_, err = stream.Recv()
	assert.ErrorIs(t, err, io.EOF)
}

func TestTextMessageCreatesTextPart(t *testing.T) {
	t.Parallel()

	message := llm.TextMessage(llm.RoleUser, "hello")

	assert.Equal(t, llm.RoleUser, message.Role)
	require.Len(t, message.Content, 1)
	assert.Equal(t, llm.PartText, message.Content[0].Type)
	assert.Equal(t, "hello", message.Content[0].Text)
	assert.Nil(t, message.Content[0].Metadata)
	assert.Nil(t, message.Content[0].ToolCall)
	assert.Nil(t, message.Content[0].ToolResult)
}

func TestPartKinds(t *testing.T) {
	t.Parallel()

	parts := []llm.Part{
		{
			Metadata:   map[string]any{"source": "fixture"},
			ToolCall:   nil,
			ToolResult: nil,
			Type:       llm.PartImage,
			Text:       "",
			Data:       "base64-image",
			MIMEType:   "image/png",
		},
		{
			Metadata:   nil,
			ToolCall:   nil,
			ToolResult: nil,
			Type:       llm.PartFile,
			Text:       "",
			Data:       "document",
			MIMEType:   "text/markdown",
		},
		{
			Metadata:   nil,
			ToolCall:   nil,
			ToolResult: nil,
			Type:       llm.PartSource,
			Text:       "citation",
			Data:       "",
			MIMEType:   "",
		},
		{
			Metadata: nil,
			ToolCall: &llm.ToolCall{
				Metadata:      nil,
				Arguments:     map[string]any{"path": "README.md"},
				ID:            "call_1",
				Name:          "read",
				ArgumentsJSON: `{"path":"README.md"}`,
			},
			ToolResult: nil,
			Type:       llm.PartToolCall,
			Text:       "",
			Data:       "",
			MIMEType:   "",
		},
		{
			Metadata: nil,
			ToolCall: nil,
			ToolResult: &llm.ToolResult{
				Metadata:      nil,
				ToolCallID:    "call_1",
				ArgumentsJSON: `{"path":"README.md"}`,
				Name:          "read",
				Error:         "",
				Content: []llm.Part{
					llm.TextPart("contents"),
				},
				IsError: false,
			},
			Type:     llm.PartToolResult,
			Text:     "",
			Data:     "",
			MIMEType: "",
		},
	}

	assert.Len(t, parts, 5)
	assert.Equal(t, llm.PartImage, parts[0].Type)
	assert.Equal(t, llm.PartFile, parts[1].Type)
	assert.Equal(t, llm.PartSource, parts[2].Type)
	assert.Equal(t, llm.PartToolCall, parts[3].Type)
	assert.Equal(t, llm.PartToolResult, parts[4].Type)
}

func TestFinishReasonValues(t *testing.T) {
	t.Parallel()

	assert.Equal(t, llm.FinishReasonUnknown, llm.FinishReason(""))
	assert.Equal(t, llm.FinishReasonStop, llm.FinishReason("stop"))
	assert.Equal(t, llm.FinishReasonLength, llm.FinishReason("length"))
	assert.Equal(t, llm.FinishReasonToolCalls, llm.FinishReason("tool-calls"))
	assert.Equal(t, llm.FinishReasonContentFilter, llm.FinishReason("content-filter"))
	assert.Equal(t, llm.FinishReasonRefusal, llm.FinishReason("refusal"))
	assert.Equal(t, llm.FinishReasonError, llm.FinishReason("error"))
	assert.Equal(t, llm.FinishReasonAborted, llm.FinishReason("aborted"))
}

func TestUsageHelpers(t *testing.T) {
	t.Parallel()

	empty := llm.EmptyUsage()
	assert.False(t, empty.HasAny())
	assert.Zero(t, empty.TotalTokens())
	assert.Zero(t, empty.ContextPercent())

	reported := llm.Usage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   100,
		ContextTokens:   25,
		InputTokens:     10,
		OutputTokens:    3,
	}
	assert.True(t, reported.HasAny())
	assert.Equal(t, 13, reported.TotalTokens())
	assert.Equal(t, 25, reported.ContextPercent())

	overWindow := llm.Usage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   100,
		ContextTokens:   150,
		InputTokens:     0,
		OutputTokens:    0,
	}
	assert.Equal(t, 100, overWindow.ContextPercent())

	inputOnly := llm.Usage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   0,
		ContextTokens:   0,
		InputTokens:     7,
		OutputTokens:    0,
	}
	assert.True(t, inputOnly.HasAny())
	assert.Equal(t, 7, inputOnly.TotalTokens())
	assert.Zero(t, inputOnly.ContextPercent())

	metadataOnly := llm.Usage{
		Breakdown:       map[string]int{"tools": 3},
		TopContributors: nil,
		ContextWindow:   0,
		ContextTokens:   0,
		InputTokens:     0,
		OutputTokens:    0,
	}
	assert.True(t, metadataOnly.HasAny())

	contributorsOnly := llm.Usage{
		Breakdown:       nil,
		TopContributors: []llm.TokenContributor{{Label: "system", Role: "system", Preview: "", Tokens: 3, Chars: 12}},
		ContextWindow:   0,
		ContextTokens:   0,
		InputTokens:     0,
		OutputTokens:    0,
	}
	assert.True(t, contributorsOnly.HasAny())
}

func TestProviderError(t *testing.T) {
	t.Parallel()

	cause := errors.New("wrapped")
	err := &llm.ProviderError{
		Cause:        cause,
		Metadata:     map[string]any{"request_id": "req_1"},
		Kind:         llm.ErrorKindRateLimit,
		Provider:     "openai",
		Model:        "gpt-test",
		Code:         "rate_limited",
		ProviderCode: "rate_limit_exceeded",
		Message:      "slow down",
		StatusCode:   429,
	}

	assert.Equal(t, "slow down", err.Error())
	require.ErrorIs(t, err, cause)
	assert.True(t, llm.IsKind(err, llm.ErrorKindRateLimit))
	assert.False(t, llm.IsKind(err, llm.ErrorKindAuth))
	unwrapped, ok := llm.AsProviderError(err)
	require.True(t, ok)
	assert.Same(t, err, unwrapped)
}

func TestProviderErrorFallbackMessages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err  *llm.ProviderError
		name string
		want string
	}{
		{name: "nil", err: nil, want: ""},
		{
			name: "cause",
			err: &llm.ProviderError{
				Cause:        errors.New("transport failed"),
				Metadata:     nil,
				Kind:         llm.ErrorKindNetwork,
				Provider:     "",
				Model:        "",
				Code:         "",
				ProviderCode: "",
				Message:      "",
				StatusCode:   0,
			},
			want: "transport failed",
		},
		{
			name: "code",
			err: &llm.ProviderError{
				Cause:        nil,
				Metadata:     nil,
				Kind:         llm.ErrorKindBadRequest,
				Provider:     "",
				Model:        "",
				Code:         "bad_request",
				ProviderCode: "",
				Message:      "",
				StatusCode:   0,
			},
			want: "provider error: bad_request",
		},
		{
			name: "empty",
			err: &llm.ProviderError{
				Cause:        nil,
				Metadata:     nil,
				Kind:         llm.ErrorKindUnknown,
				Provider:     "",
				Model:        "",
				Code:         "",
				ProviderCode: "",
				Message:      "",
				StatusCode:   0,
			},
			want: "provider error",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := ""
			if test.err != nil {
				got = test.err.Error()
			}
			assert.Equal(t, test.want, got)
		})
	}
}

func TestProviderErrorNilHelpers(t *testing.T) {
	t.Parallel()

	var providerError *llm.ProviderError
	require.NoError(t, providerError.Unwrap())
	assert.False(t, llm.IsKind(assert.AnError, llm.ErrorKindUnknown))
	converted, ok := llm.AsProviderError(assert.AnError)
	assert.False(t, ok)
	assert.Nil(t, converted)
}
