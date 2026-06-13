package llm_test

import (
	"context"
	"io"

	"github.com/omarluq/librecode/internal/llm"
)

type testGenerator struct{}

func (testGenerator) Generate(context.Context, *llm.Request) (*llm.Response, error) {
	return &llm.Response{
		FinishReason: llm.FinishReasonStop,
		Content:      []llm.Part{llm.TextPart("done")},
		ToolCalls:    nil,
		Usage:        llm.EmptyUsage(),
	}, nil
}

type testStreamer struct{}

func (testStreamer) Stream(context.Context, llm.Request) (*testStream, error) {
	return &testStream{sent: false}, nil
}

type testStream struct {
	sent bool
}

func (stream *testStream) Recv() (*llm.StreamChunk, error) {
	if stream.sent {
		return nil, io.EOF
	}

	stream.sent = true

	return &llm.StreamChunk{
		Part:         nil,
		ToolCall:     nil,
		FinishReason: llm.FinishReasonToolCalls,
		Usage:        llm.EmptyUsage(),
	}, nil
}

func (*testStream) Close() error {
	return nil
}
