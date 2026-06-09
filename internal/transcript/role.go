package transcript

import "github.com/omarluq/librecode/internal/database"

// Role identifies a transcript message category independent from storage roles.
type Role string

const (
	// RoleUser is a user-authored prompt.
	RoleUser Role = "user"
	// RoleAssistant is an assistant response.
	RoleAssistant Role = "assistant"
	// RoleToolResult is output from a tool execution.
	RoleToolResult Role = "toolResult"
	// RoleThinking is model reasoning or thinking text.
	RoleThinking Role = "thinking"
	// RoleCustom is extension-provided context.
	RoleCustom Role = "custom"
	// RoleBashExecution is output from a user-run shell command.
	RoleBashExecution Role = "bashExecution"
	// RoleBranchSummary is summary context for an abandoned branch.
	RoleBranchSummary Role = "branchSummary"
	// RoleCompactionSummary is summary context for compacted history.
	RoleCompactionSummary Role = "compactionSummary"
)

// FromDatabaseRole converts a durable database role into a transcript role.
func FromDatabaseRole(role database.Role) Role {
	return Role(role)
}

// ToDatabaseRole converts a transcript role into a durable database role.
func ToDatabaseRole(role Role) database.Role {
	return database.Role(role)
}

// CanMergeStreamingRole reports whether adjacent streaming transcript blocks with
// the same role can be joined without changing their meaning.
func CanMergeStreamingRole(role Role) bool {
	switch role {
	case RoleAssistant, RoleThinking:
		return true
	case RoleUser,
		RoleToolResult,
		RoleCustom,
		RoleBashExecution,
		RoleBranchSummary,
		RoleCompactionSummary:
		return false
	default:
		return false
	}
}
