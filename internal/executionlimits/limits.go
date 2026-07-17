// Package executionlimits defines fixed safety budgets for isolated MVM execution.
package executionlimits

const (
	// MaxFrameSize is the largest encoded worker protocol frame.
	MaxFrameSize = 8 << 20
	// MaxResultSize is the largest encoded evaluation value in a result frame.
	MaxResultSize = 1 << 20
	// responseEnvelopeBudget reserves space for result metadata and JSON framing.
	responseEnvelopeBudget = 1 << 20
	// MaxOutputSize leaves worst-case JSON escaping headroom for captured output.
	MaxOutputSize = (MaxFrameSize - MaxResultSize - responseEnvelopeBudget) / 6
)
