package contextwindow

const (
	// defaultRecentTailTokens is used when no model context window is available.
	defaultRecentTailTokens = 20_000
	contextTailDivisor      = 3
)

// RecentTailInput describes the policy inputs for selecting the verbatim tail kept during compaction.
type RecentTailInput struct {
	ContextWindow int
	CurrentTokens int
}

// RecentTailTarget returns the number of newest tokens to keep untouched during compaction.
func RecentTailTarget(input RecentTailInput) int {
	contextTail := defaultRecentTailTokens
	if input.ContextWindow > 0 {
		contextTail = max(1, input.ContextWindow/contextTailDivisor)
	}

	if input.CurrentTokens <= 0 {
		return contextTail
	}

	currentTail := max(1, (input.CurrentTokens+contextTailDivisor-1)/contextTailDivisor)

	return min(contextTail, currentTail)
}
