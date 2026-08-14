package compaction

import (
	"errors"
	"fmt"

	"github.com/omarluq/librecode/internal/llm"
)

var (
	// ErrSummaryInputOverflow indicates that summary input exceeds its budget.
	ErrSummaryInputOverflow = errors.New("summary input overflow")
	// ErrSummaryOutputTruncated indicates that the provider truncated summary output.
	ErrSummaryOutputTruncated = errors.New("summary output truncated")
	// ErrSummaryEmpty indicates that the provider returned an empty summary.
	ErrSummaryEmpty = errors.New("compaction summary was empty")
	// ErrSummaryContentFilter indicates that provider policy filtered the summary.
	ErrSummaryContentFilter = errors.New("compaction summary was content filtered")
	// ErrSummaryRefusal indicates that the model refused to produce the summary.
	ErrSummaryRefusal = errors.New("compaction summary was refused")
	// ErrSummaryProviderFailure indicates that generation ended without a usable summary.
	ErrSummaryProviderFailure = errors.New("compaction summary provider failure")
	// ErrSummaryAborted indicates that summary generation was canceled or aborted.
	ErrSummaryAborted = errors.New("compaction summary was aborted")
	// ErrSummaryReductionNoProgress indicates that reduction did not shrink the input.
	ErrSummaryReductionNoProgress = errors.New("summary reduction made no progress")
	// ErrSummaryFixedOverhead indicates that fixed prompt content exceeds the input budget.
	ErrSummaryFixedOverhead = errors.New("summary fixed overhead exceeds budget")
	// ErrSummaryIndivisibleGroup indicates that one semantic group exceeds the input budget.
	ErrSummaryIndivisibleGroup = errors.New("summary semantic group exceeds budget")
)

// SummaryOutcome records bounded summary generation without retaining prompt content.
type SummaryOutcome struct {
	Text                 string
	Provider             string
	Model                string
	Reason               Reason
	FinishReason         llm.FinishReason
	EstimatedInputTokens int
	ReportedInputTokens  int
	ReportedOutputTokens int
	OutputLimit          int
	Truncated            bool
}

// SummaryError provides safe, typed summary failure context.
type SummaryError struct {
	Kind     error
	Cause    error
	Provider string
	Model    string
	Reason   Reason
	Input    int
	Limit    int
	Before   int
	After    int
}

func (summaryError *SummaryError) Error() string {
	if summaryError == nil {
		return "summary failed"
	}

	return fmt.Sprintf("%v (provider=%q model=%q reason=%q input=%d limit=%d before=%d after=%d)",
		summaryError.Kind, summaryError.Provider, summaryError.Model, summaryError.Reason,
		summaryError.Input, summaryError.Limit, summaryError.Before, summaryError.After)
}

// Unwrap exposes both the stable category and an optional underlying cause.
func (summaryError *SummaryError) Unwrap() []error {
	if summaryError == nil {
		return nil
	}

	if summaryError.Cause == nil {
		return []error{summaryError.Kind}
	}

	return []error{summaryError.Kind, summaryError.Cause}
}
