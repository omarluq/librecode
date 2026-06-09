package contextwindow

import (
	"github.com/omarluq/librecode/internal/core"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

const (
	// ContributionSourceExtension is the default source for extension-provided context.
	ContributionSourceExtension = "extension"
	// ContributionRoleSystem is the default role for extension-provided context.
	ContributionRoleSystem = "system"
	// ContributionMaxTokens limits a single extension contribution.
	ContributionMaxTokens = 2048
	// BreakdownSystem is the token breakdown key for the base system prompt.
	BreakdownSystem = "system"
	// BreakdownSkills is the token breakdown key for available and active skills.
	BreakdownSkills = "skills"
	// BreakdownHistory is the token breakdown key for conversation history.
	BreakdownHistory = "history"
	// BreakdownExtensions is the token breakdown key for extension context.
	BreakdownExtensions = "extensions"
)

// Contribution describes extension-provided context appended to the system prompt.
type Contribution struct {
	Metadata map[string]any
	Source   string
	Name     string
	Role     string
	Content  string
	Tokens   int
}

// BuildResult is the model-facing context assembled for a provider request.
type BuildResult struct {
	Breakdown     map[string]int
	SystemPrompt  string
	Contributions []Contribution
	Messages      []database.MessageEntity
	UsageAnchor   *database.ContextUsageAnchorEntity
	Usage         model.TokenUsage
}

// Base describes context inputs before extension context-build hooks mutate them.
type Base struct {
	UsageAnchor      *database.ContextUsageAnchorEntity
	BaseSystemPrompt string
	SkillPrompt      string
	SystemPrompt     string
	ActiveSkills     []core.ActivatedSkill
	SkillDiagnostics []core.SkillActivationDiagnostic
	Messages         []database.MessageEntity
	HistoryTokens    int
	SystemTokens     int
	SkillTokens      int
}
