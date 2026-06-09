package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/llm"
)

func TestRequestToolDefinitionsUsesLLMDefinitions(t *testing.T) {
	t.Parallel()

	request := &CompletionRequest{
		OnProviderObserve: nil,
		OnProviderRequest: nil,
		ExecuteTools:      nil,
		OnEvent:           nil,
		Request: llm.Request{
			ProviderOptions: nil,
			Auth:            llm.Auth{Headers: nil, APIKey: ""},
			SystemPrompt:    "",
			ThinkingLevel:   "",
			SessionID:       "",
			Messages:        nil,
			Tools: []llm.ToolDefinition{{
				Schema:      map[string]any{jsonTypeKey: jsonObjectType},
				Name:        "custom",
				Description: "custom tool",
				ReadOnly:    true,
			}},
			Model:        emptyModelRef(),
			Usage:        llm.EmptyUsage(),
			DisableTools: false,
		},
		ProviderAttempt: 0,
	}

	definitions := requestToolDefinitions(request)

	assert.Len(t, definitions, 1)
	assert.Equal(t, "custom", definitions[0].Name)
	assert.True(t, definitions[0].ReadOnly)
}
