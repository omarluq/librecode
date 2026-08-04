package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/llm"
)

const (
	testImageMIME       = "image/png"
	testImageData       = "AQID"
	testImageDataURL    = "data:image/png;base64,AQID"
	testImagePrompt     = "describe image"
	testImageWithText   = "text and image"
	testImageOnly       = "image only"
	testAssistantReplay = "answer"
)

func testImageMessage() llm.Message {
	return llm.Message{Metadata: nil, Role: llm.RoleUser, Content: []llm.Part{
		llm.TextPart(testImagePrompt),
		{Metadata: nil, ToolCall: nil, ToolResult: nil, Type: llm.PartImage,
			Text: "", MIMEType: testImageMIME, Data: testImageData},
	}}
}

func testImageOnlyMessage(role llm.Role, mimeType, data string) llm.Message {
	return llm.Message{Metadata: nil, Role: role, Content: []llm.Part{{
		Metadata: nil, ToolCall: nil, ToolResult: nil, Type: llm.PartImage,
		Text: "", MIMEType: mimeType, Data: data,
	}}}
}

func TestMultipartProviderMappings(t *testing.T) {
	t.Parallel()

	message := testImageMessage()
	responses, err := openAIResponseInput([]llm.Message{message})
	require.NoError(t, err)

	responseObject, converted := responses[0].(map[string]any)
	require.True(t, converted)

	responseContent, contentConverted := responseObject[jsonContentKey].([]map[string]any)
	require.True(t, contentConverted)
	assert.Equal(t, "input_text", responseContent[0][jsonTypeKey])
	assert.Equal(t, "input_image", responseContent[1][jsonTypeKey])
	assert.Equal(t, "data:image/png;base64,AQID", responseContent[1][jsonImageURLKey])

	request := emptyCompletionRequest()
	setTestRequestMessages(request, []llm.Message{message})
	chat, err := openAIChatMessages(request)
	require.NoError(t, err)

	chatContent, chatConverted := chat[0][jsonContentKey].([]map[string]any)
	require.True(t, chatConverted)

	imageURL, imageURLConverted := chatContent[1][jsonImageURLKey].(map[string]any)
	require.True(t, imageURLConverted)
	assert.Equal(t, "data:image/png;base64,AQID", imageURL["url"])

	anthropic, err := anthropicMessages([]llm.Message{message})
	require.NoError(t, err)

	anthropicContent, anthropicConverted := anthropic[0][jsonContentKey].([]map[string]any)
	require.True(t, anthropicConverted)

	source, sourceConverted := anthropicContent[1]["source"].(map[string]any)
	require.True(t, sourceConverted)
	assert.Equal(t, "AQID", source["data"])
}

func TestMultipartProviderMappingsPreserveImageOnlyOrder(t *testing.T) {
	t.Parallel()

	message := llm.Message{Metadata: nil, Role: llm.RoleUser, Content: []llm.Part{
		{Metadata: nil, ToolCall: nil, ToolResult: nil, Type: llm.PartImage,
			Text: "", MIMEType: testImageMIME, Data: testImageData},
		{Metadata: nil, ToolCall: nil, ToolResult: nil, Type: llm.PartImage,
			Text: "", MIMEType: "image/jpeg", Data: "BAUG"},
	}}

	responses, err := openAIResponseInput([]llm.Message{message})
	require.NoError(t, err)

	responseObject, responseObjectOK := responses[0].(map[string]any)
	require.True(t, responseObjectOK)

	responseContent, responseContentOK := responseObject[jsonContentKey].([]map[string]any)
	require.True(t, responseContentOK)
	assert.Equal(t, []any{testImageDataURL, "data:image/jpeg;base64,BAUG"}, []any{
		responseContent[0][jsonImageURLKey], responseContent[1][jsonImageURLKey],
	})

	request := emptyCompletionRequest()
	setTestRequestMessages(request, []llm.Message{message})
	chat, err := openAIChatMessages(request)
	require.NoError(t, err)

	chatContent, chatContentOK := chat[0][jsonContentKey].([]map[string]any)
	require.True(t, chatContentOK)
	assert.Len(t, chatContent, 2)

	anthropic, err := anthropicMessages([]llm.Message{message})
	require.NoError(t, err)

	anthropicContent, anthropicContentOK := anthropic[0][jsonContentKey].([]map[string]any)
	require.True(t, anthropicContentOK)
	assert.Len(t, anthropicContent, 2)
}

func TestMultipartProviderMappingsSkipEmptyUserMessages(t *testing.T) {
	t.Parallel()

	empty := llm.Message{Metadata: nil, Role: llm.RoleUser, Content: []llm.Part{}}

	responses, err := openAIResponseInput([]llm.Message{empty})
	require.NoError(t, err)
	assert.Empty(t, responses)

	request := emptyCompletionRequest()
	setTestRequestMessages(request, []llm.Message{empty})
	chat, err := openAIChatMessages(request)
	require.NoError(t, err)
	assert.Empty(t, chat)

	anthropic, err := anthropicMessages([]llm.Message{empty})
	require.NoError(t, err)
	assert.Empty(t, anthropic)
}

func TestMultipartProviderMappingsPreserveTextOnlyPayloads(t *testing.T) {
	t.Parallel()

	message := llm.TextMessage(llm.RoleUser, "plain text")
	responses, err := openAIResponseInput([]llm.Message{message})
	require.NoError(t, err)

	responseObject, responseObjectOK := responses[0].(map[string]any)
	require.True(t, responseObjectOK)

	responseContent, responseContentOK := responseObject[jsonContentKey].([]map[string]any)
	require.True(t, responseContentOK)
	assert.Equal(t, "plain text", responseContent[0][jsonTextKey])

	request := emptyCompletionRequest()
	setTestRequestMessages(request, []llm.Message{message})
	chat, err := openAIChatMessages(request)
	require.NoError(t, err)
	assert.Equal(t, "plain text", chat[0][jsonContentKey])

	anthropic, err := anthropicMessages([]llm.Message{message})
	require.NoError(t, err)
	assert.Equal(t, "plain text", anthropic[0][jsonContentKey])
}

func TestMultipartProviderMappingsRejectMalformedAndUnsupportedRole(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		message llm.Message
	}{
		{name: "empty data", message: testImageOnlyMessage(llm.RoleUser, testImageMIME, "")},
		{name: "malformed base64", message: testImageOnlyMessage(llm.RoleUser, testImageMIME, "not base64!")},
		{name: "unsupported MIME", message: testImageOnlyMessage(llm.RoleUser, "image/svg+xml", testImageData)},
		{name: "assistant role", message: testImageOnlyMessage(llm.RoleAssistant, testImageMIME, testImageData)},
		{name: "tool role", message: testImageOnlyMessage(llm.RoleTool, testImageMIME, testImageData)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			for _, convert := range imageConverters(test.message) {
				err := convert()
				require.Error(t, err)
			}
		})
	}
}

func TestProvidersSkipEmptyStructuredUserContent(t *testing.T) {
	t.Parallel()

	message := llm.Message{Metadata: nil, Role: llm.RoleUser, Content: []llm.Part{{
		Metadata: nil, ToolCall: nil, ToolResult: nil, Type: llm.PartFile,
		Text: "", MIMEType: "", Data: "",
	}}}

	responses, err := openAIResponseInput([]llm.Message{message})
	require.NoError(t, err)
	assert.Empty(t, responses)

	request := emptyCompletionRequest()
	setTestRequestMessages(request, []llm.Message{message})
	chat, err := openAIChatMessages(request)
	require.NoError(t, err)
	assert.Empty(t, chat)

	anthropic, err := anthropicMessages([]llm.Message{message})
	require.NoError(t, err)
	assert.Empty(t, anthropic)
}

func TestCodexCompactionRejectsAssistantImagesBeforeFlattening(t *testing.T) {
	t.Parallel()

	_, err := compactCodexResponseMessages([]llm.Message{
		testImageOnlyMessage(llm.RoleAssistant, testImageMIME, testImageData),
	})
	require.Error(t, err)
}

func imageConverters(message llm.Message) []func() error {
	return []func() error{
		func() error {
			_, err := openAIResponseInput([]llm.Message{message})

			return err
		},
		func() error {
			request := emptyCompletionRequest()
			setTestRequestMessages(request, []llm.Message{message})
			_, err := openAIChatMessages(request)

			return err
		},
		func() error {
			_, err := anthropicMessages([]llm.Message{message})

			return err
		},
	}
}
