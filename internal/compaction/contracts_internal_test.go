package compaction

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContractsValidate(t *testing.T) {
	t.Parallel()

	for _, reason := range []Reason{ReasonManual, ReasonPreRequest, ReasonPostResponse, ReasonProviderOverflow} {
		require.NoError(t, reason.Validate())
	}

	require.Error(t, Reason("unknown").Validate())

	for _, intent := range []RetryIntent{RetryNone, RetryAfterCompaction} {
		require.NoError(t, intent.Validate())
	}

	require.Error(t, RetryIntent("again").Validate())
}

func TestSummaryErrorUnwrapOmitsNilAndPreservesOrder(t *testing.T) {
	t.Parallel()

	kind := errors.New("kind")
	cause := errors.New("cause")
	tests := []struct {
		name string
		err  *SummaryError
		want []error
	}{
		{name: "neither", err: testSummaryError(nil, nil), want: []error{}},
		{name: "kind", err: testSummaryError(kind, nil), want: []error{kind}},
		{name: "cause", err: testSummaryError(nil, cause), want: []error{cause}},
		{name: "both", err: testSummaryError(kind, cause), want: []error{kind, cause}},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, testCase.want, testCase.err.Unwrap())
		})
	}
}

func testSummaryError(kind, cause error) *SummaryError {
	return &SummaryError{
		Kind: kind, Cause: cause, Provider: "", Model: "", Reason: "",
		Input: 0, Limit: 0, Before: 0, After: 0,
	}
}

func TestSummaryErrorSupportsIsAndAs(t *testing.T) {
	t.Parallel()

	cause := errors.New("transport")
	for _, kind := range []error{ErrSummaryInputOverflow, ErrSummaryOutputTruncated, ErrSummaryEmpty,
		ErrSummaryReductionNoProgress, ErrSummaryFixedOverhead, ErrSummaryIndivisibleGroup} {
		err := &SummaryError{
			Kind: kind, Cause: cause, Provider: "provider", Model: "model", Reason: ReasonManual,
			Input: 0, Limit: 0, Before: 0, After: 0,
		}
		require.ErrorIs(t, err, kind)
		require.ErrorIs(t, err, cause)

		var summaryError *SummaryError
		require.ErrorAs(t, err, &summaryError)
		assert.NotContains(t, err.Error(), "credential")
	}
}
