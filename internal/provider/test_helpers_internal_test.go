package provider

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/tool"
)

const (
	testBranchContent       = "branch"
	testCompactionContent   = "compaction"
	testCustomContent       = "custom"
	testExistingKey         = "existing"
	testThinkingDelta       = "thinking"
	testOpenAIProvider      = "openai"
	testProviderAPIKey      = "sk-test"
	testProviderAccountID   = "account-test"
	testProviderBaseURL     = "https://provider.test"
	testProviderMessageType = jsonMessageType
	testProviderPartialText = "partial"
	testProviderDeclined    = "declined"
	testProviderHello       = "hello"
	testToolArgumentsJSON   = `{"path":"README.md"}`
)

type providerResponse struct {
	Text       string
	Thinking   []string
	ToolEvents []ToolEvent
	Usage      llm.Usage
}

func providerResponseView(response *llm.Response) providerResponse {
	if response == nil {
		return providerResponse{Text: "", Thinking: nil, ToolEvents: nil, Usage: llm.EmptyUsage()}
	}

	return providerResponse{
		Text:       responseText(response),
		Thinking:   responseThinking(response),
		ToolEvents: responseToolEvents(response),
		Usage:      response.Usage,
	}
}

func responseText(response *llm.Response) string {
	if response == nil {
		return ""
	}

	return partsText(response.Content)
}

func responseThinking(response *llm.Response) []string {
	if response == nil {
		return nil
	}

	thinking := []string{}

	for _, part := range response.Content {
		if part.Type == llm.PartReasoning && strings.TrimSpace(part.Text) != "" {
			thinking = append(thinking, strings.TrimSpace(part.Text))
		}
	}

	if len(thinking) == 0 {
		return nil
	}

	return thinking
}

func responseToolEvents(response *llm.Response) []ToolEvent {
	if response == nil {
		return nil
	}

	events := []ToolEvent{}

	for _, part := range response.Content {
		if part.Type == llm.PartToolResult && part.ToolResult != nil {
			events = append(events, toolEventFromLLM(part.ToolResult))
		}
	}

	if len(events) == 0 {
		return nil
	}

	return events
}

func emptyCompletionRequest() *CompletionRequest {
	return &CompletionRequest{
		OnProviderObserve: nil,
		OnProviderRequest: nil,
		ExecuteTools:      nil,
		OnEvent:           nil,
		Request:           emptyRequest(),
		ProviderAttempt:   0,
	}
}

func setTestRequestProvider(request *CompletionRequest, provider string) {
	request.Request.Model.Provider = provider
}

func setTestRequestAPI(request *CompletionRequest, api string) {
	request.Request.Model.API = api
}

func setTestRequestBaseURL(request *CompletionRequest, baseURL string) {
	request.Request.Model.BaseURL = baseURL
}

func setTestRequestModelID(request *CompletionRequest, modelID string) {
	request.Request.Model.ID = modelID
}

func setTestRequestReasoning(request *CompletionRequest, reasoning bool) {
	request.Request.Model.Reasoning = reasoning
}

func setTestRequestThinkingLevel(request *CompletionRequest, level string) {
	request.Request.ThinkingLevel = level
}

func setTestRequestSystemPrompt(request *CompletionRequest, prompt string) {
	request.Request.SystemPrompt = prompt
}

func setTestRequestMessages(request *CompletionRequest, messages []llm.Message) {
	request.Request.Messages = messages
}

func setTestRequestCWD(request *CompletionRequest, cwd string) {
	if request.Request.ProviderOptions == nil {
		request.Request.ProviderOptions = map[string]any{}
	}

	request.Request.ProviderOptions["cwd"] = cwd
}

func testRequestCWD(request *CompletionRequest) string {
	if request == nil || request.Request.ProviderOptions == nil {
		return ""
	}

	cwd, ok := request.Request.ProviderOptions["cwd"].(string)
	if !ok {
		return ""
	}

	return cwd
}

func installTestToolExecutor(request *CompletionRequest) {
	registry := tool.NewRegistry(testRequestCWD(request))
	request.ExecuteTools = func(
		ctx context.Context,
		calls []llm.ToolCall,
		onEvent func(*llm.StreamChunk),
	) ([]llm.ToolResult, error) {
		if registry == nil {
			return nil, oops.In("provider").Code("tool_registry_missing").Errorf("tool registry is not configured")
		}

		results := make([]llm.ToolResult, 0, len(calls))
		for _, call := range calls {
			emitLLMToolStart(onEvent, call.Name)
			result, err := registry.Execute(ctx, call.Name, call.Arguments)
			toolResult := llmToolResultFromExecution(&call, result, err)
			emitLLMToolResult(onEvent, &toolResult)
			results = append(results, toolResult)
		}

		return results, nil
	}
}

func emitLLMToolStart(onEvent func(*llm.StreamChunk), name string) {
	if onEvent == nil {
		return
	}

	part := llm.TextPart(name)
	onEvent(&llm.StreamChunk{
		Part:         &part,
		ToolCall:     nil,
		FinishReason: llm.FinishReasonUnknown,
		Usage:        llm.EmptyUsage(),
	})
}

func emitLLMToolResult(onEvent func(*llm.StreamChunk), result *llm.ToolResult) {
	if onEvent == nil {
		return
	}

	part := llm.Part{
		Metadata:   nil,
		ToolCall:   nil,
		ToolResult: result,
		Type:       llm.PartToolResult,
		Text:       "",
		Data:       "",
		MIMEType:   "",
	}
	onEvent(&llm.StreamChunk{
		Part:         &part,
		ToolCall:     nil,
		FinishReason: llm.FinishReasonUnknown,
		Usage:        llm.EmptyUsage(),
	})
}

func llmToolResultFromExecution(call *llm.ToolCall, result tool.Result, err error) llm.ToolResult {
	text := result.Text()
	errorText := ""

	if err != nil {
		text = err.Error()
		errorText = err.Error()
	}

	if strings.TrimSpace(text) == "" {
		text = "(tool returned no text output)"
	}

	metadata := map[string]any{}
	if details := encodeToolDetails(result.Details); details != "" {
		metadata["details_json"] = details
	}

	if len(metadata) == 0 {
		metadata = nil
	}

	toolCallID := ""
	argumentsJSON := ""
	name := ""

	if call != nil {
		toolCallID = call.ID
		argumentsJSON = call.ArgumentsJSON
		name = call.Name
	}

	return llm.ToolResult{
		Metadata:      metadata,
		ToolCallID:    toolCallID,
		ArgumentsJSON: argumentsJSON,
		Name:          name,
		Error:         errorText,
		Content:       []llm.Part{llm.TextPart(text)},
		IsError:       err != nil,
	}
}

func setTestThinkingMap(request *CompletionRequest, level, value string) {
	if request.Request.Model.ThinkingLevelMap == nil {
		request.Request.Model.ThinkingLevelMap = map[string]*string{}
	}

	trimmed := strings.TrimSpace(value)
	request.Request.Model.ThinkingLevelMap[level] = &trimmed
}

func jsonString(value any) string {
	bytes, err := json.Marshal(value)
	if err != nil {
		panic("jsonString: failed to marshal test value: " + err.Error())
	}

	return string(bytes)
}

const (
	expectedReadToolName    = string(jsonReadToolName)
	expectedWriteToolName   = string(jsonWriteToolName)
	expectedBashToolName    = string(jsonBashToolName)
	expectedFindToolName    = string(jsonFindToolName)
	expectedPathKey         = string(jsonPathKey)
	expectedAllowIgnoredKey = string(jsonAllowIgnoredKey)
	expectedCommandKey      = string(jsonCommandKey)
)
