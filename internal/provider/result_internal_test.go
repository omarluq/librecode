package provider

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/llm"
)

func TestResponseResultHelpers(t *testing.T) {
	t.Parallel()

	response := newResponse()
	assert.Equal(t, llm.FinishReasonUnknown, response.FinishReason)
	assert.Equal(t, llm.EmptyUsage(), response.Usage)

	blank := strings.Repeat(" ", 3)
	appendThinking(response, []string{"  reason  ", blank})
	setResponseText(response, " hello ")
	setResponseText(response, blank)
	appendToolResults(response, []ToolEvent{{
		Name:          jsonReadToolName,
		ArgumentsJSON: `{"path":"README.md"}`,
		DetailsJSON:   `{"path":"README.md"}`,
		Result:        "contents",
		Error:         "",
		IsError:       false,
	}})

	assert.Equal(t, "reason\nhello", responseText(response))
	assert.Equal(t, []string{"reason"}, responseThinking(response))
	events := responseToolEvents(response)
	require.Len(t, events, 1)
	assert.Equal(t, expectedReadToolName, events[0].Name)
	assert.Equal(t, "contents", events[0].Result)
	assert.JSONEq(t, `{"path":"README.md"}`, events[0].DetailsJSON)
}

func TestResponseResultHelpersHandleEmptyInputs(t *testing.T) {
	t.Parallel()

	assert.Empty(t, responseText(nil))
	assert.Nil(t, responseThinking(nil))
	assert.Nil(t, responseToolEvents(nil))
	plainResponse := &llm.Response{
		FinishReason: llm.FinishReasonUnknown,
		Content:      []llm.Part{llm.TextPart("plain")},
		ToolCalls:    nil,
		Usage:        llm.EmptyUsage(),
	}
	assert.Empty(t, responseThinking(plainResponse))
	assert.Nil(t, responseToolEvents(plainResponse))
}

func TestPartsTextSkipsBlankText(t *testing.T) {
	t.Parallel()

	parts := []llm.Part{
		llm.TextPart(" first "),
		llm.TextPart("   "),
		{
			Metadata:   nil,
			ToolCall:   nil,
			ToolResult: nil,
			Type:       llm.PartReasoning,
			Text:       " second ",
			Data:       "",
			MIMEType:   "",
		},
	}

	assert.Equal(t, "first\nsecond", partsText(parts))
}
