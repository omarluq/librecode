package llm

import "github.com/omarluq/librecode/internal/mapsutil"

// MergeUsage overlays provider-reported usage on an estimated usage snapshot.
func MergeUsage(estimated, reported Usage) Usage {
	usage := estimated
	if reported.ContextWindow > 0 {
		usage.ContextWindow = reported.ContextWindow
	}

	if reported.ContextTokens > 0 {
		usage.ContextTokens = reported.ContextTokens
	}

	if reported.InputTokens > 0 {
		usage.InputTokens = reported.InputTokens
	}

	if reported.OutputTokens > 0 {
		usage.OutputTokens = reported.OutputTokens
	}

	if len(usage.Breakdown) == 0 && len(reported.Breakdown) > 0 {
		usage.Breakdown = mapsutil.CloneOrNil(reported.Breakdown)
	}

	if len(usage.TopContributors) == 0 && len(reported.TopContributors) > 0 {
		usage.TopContributors = CloneTokenContributors(reported.TopContributors)
	}

	return usage
}
