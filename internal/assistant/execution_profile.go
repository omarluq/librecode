package assistant

import (
	"slices"

	"github.com/omarluq/librecode/internal/agent"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/tool"
)

// ExecutionKind identifies the purpose of one runtime execution.
type ExecutionKind string

const (
	// ExecutionTopLevel is an interactive user-owned prompt.
	ExecutionTopLevel ExecutionKind = "top_level"
	// ExecutionAgentTask is a durable background agent task.
	ExecutionAgentTask ExecutionKind = "agent_task"
)

// ExecutionProfile is an immutable snapshot of prompt capabilities and overrides.
type ExecutionProfile struct {
	Kind             ExecutionKind
	AgentName        string
	SystemPrompt     string
	Provider         string
	Model            string
	ThinkingLevel    model.ThinkingLevel
	PermissionMode   agent.PermissionMode
	Tools            []tool.Name
	EnableSkills     bool
	EnableExtensions bool
	MaxTurns         int
	Depth            int
}

func topLevelExecutionProfile() ExecutionProfile {
	return ExecutionProfile{
		Kind: ExecutionTopLevel, AgentName: "", SystemPrompt: "", Provider: "", Model: "",
		ThinkingLevel: "", PermissionMode: agent.PermissionAllow, Tools: nil,
		EnableSkills: true, EnableExtensions: true,
		MaxTurns: 0, Depth: 0,
	}
}

func cloneExecutionProfile(profile *ExecutionProfile) ExecutionProfile {
	cloned := *profile
	cloned.Tools = slices.Clone(profile.Tools)

	return cloned
}
