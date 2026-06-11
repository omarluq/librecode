package contextwindow

const (
	// defaultKeepRecentTokens is used when no explicit override or model context window is available.
	defaultKeepRecentTokens = 20_000
)

// RecentTailInput describes the policy inputs for selecting the verbatim tail kept during compaction.
type RecentTailInput struct {
	ExplicitKeepRecentTokens int
	ContextWindow            int
}

// RecentTailTarget returns the number of newest tokens to keep untouched during compaction.
func RecentTailTarget(input RecentTailInput) int {
	if input.ExplicitKeepRecentTokens > 0 {
		return input.ExplicitKeepRecentTokens
	}
	if input.ContextWindow <= 0 {
		return defaultKeepRecentTokens
	}

	return max(1, input.ContextWindow/3)
}
