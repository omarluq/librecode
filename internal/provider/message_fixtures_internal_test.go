package provider

import "github.com/omarluq/librecode/internal/llm"

func mixedReplayMessages() []llm.Message {
	return []llm.Message{
		llm.TextMessage(llm.RoleUser, jsonUserRole),
		llm.TextMessage(llm.RoleAssistant, jsonAssistantRole),
		llm.TextMessage(llm.RoleUser, testBranchContent),
		llm.TextMessage(llm.RoleUser, testCompactionContent),
		llm.TextMessage(llm.RoleUser, testCustomContent),
		llm.TextMessage(llm.RoleUser, jsonBashToolName),
		llm.TextMessage(llm.RoleTool, jsonToolRole),
		llm.TextMessage(llm.RoleAssistant, testThinkingDelta),
		llm.TextMessage(llm.RoleUser, ""),
	}
}
