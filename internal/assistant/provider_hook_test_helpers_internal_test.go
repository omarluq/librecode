package assistant

import "github.com/omarluq/librecode/internal/llm"

func providerHookTestInput(payload map[string]any, headers map[string]string, attempt int) *llm.HookInput {
	request := providerHookTestRequest()

	return &llm.HookInput{
		ProviderOptions: map[string]any{"cwd": request.CWD},
		Payload:         payload,
		Headers:         headers,
		SessionID:       request.SessionID,
		ThinkingLevel:   request.ThinkingLevel,
		Model:           llmModelRefFromModel(&request.Model),
		Attempt:         attempt,
	}
}
