package event_test

import (
	"context"
	"testing"
	"time"

	"github.com/samber/ro"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/event"
)

func TestContextStreams(t *testing.T) {
	t.Parallel()

	tests := []contextStreamCase{
		{
			name:          "after context emits once",
			source:        afterContextSource(time.Millisecond),
			subscriberCtx: nil,
			expectedErr:   nil,
			expected:      []int64{0},
		},
		{
			name:          "after context emits immediately for non-positive delay",
			source:        afterContextSource(0),
			subscriberCtx: nil,
			expectedErr:   nil,
			expected:      []int64{0},
		},
		{
			name:          "after context stops on source cancel",
			source:        canceledAfterContextSource(time.Hour),
			subscriberCtx: nil,
			expectedErr:   context.Canceled,
			expected:      []int64{},
		},
		{
			name:          "after context stops on subscriber cancel",
			source:        afterContextSource(time.Hour),
			subscriberCtx: canceledTestContext,
			expectedErr:   context.Canceled,
			expected:      []int64{},
		},
		{
			name:          "context done emits on cancel",
			source:        canceledContextDoneSource,
			subscriberCtx: nil,
			expectedErr:   nil,
			expected:      []int64{0},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			values, err := collectContextValues(t, testCase.source(t), testCase.subscriberCtx)

			if testCase.expectedErr != nil {
				require.ErrorIs(t, err, testCase.expectedErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, testCase.expected, values)
		})
	}
}

type contextStreamCase struct {
	source        func(t *testing.T) ro.Observable[int64]
	subscriberCtx func(t *testing.T) context.Context
	expectedErr   error
	name          string
	expected      []int64
}

func afterContextSource(delay time.Duration) func(t *testing.T) ro.Observable[int64] {
	return func(t *testing.T) ro.Observable[int64] {
		t.Helper()

		return event.AfterContext(context.Background(), delay)
	}
}

func canceledAfterContextSource(delay time.Duration) func(t *testing.T) ro.Observable[int64] {
	return func(t *testing.T) ro.Observable[int64] {
		t.Helper()

		return event.AfterContext(canceledTestContext(t), delay)
	}
}

func canceledContextDoneSource(t *testing.T) ro.Observable[int64] {
	t.Helper()

	return ro.Pipe1(
		event.ContextDone(canceledTestContext(t)),
		ro.Map(func(struct{}) int64 { return 0 }),
	)
}

func canceledTestContext(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	t.Cleanup(cancel)

	return ctx
}

func collectContextValues(
	t *testing.T,
	source ro.Observable[int64],
	subscriberCtx func(t *testing.T) context.Context,
) ([]int64, error) {
	t.Helper()

	if subscriberCtx == nil {
		return ro.Collect(source)
	}

	values, _, err := ro.CollectWithContext(subscriberCtx(t), source)
	return values, err
}
