package executeworker_test

import (
	"errors"
	"maps"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/executeworker"
	"github.com/omarluq/librecode/internal/guestapi"
	"github.com/omarluq/librecode/internal/mvmhost"
	"github.com/omarluq/librecode/internal/workflowkernel"
)

func TestCanonicalCombinatorsPreserveOrderInBothProfiles(t *testing.T) {
	t.Parallel()

	source := `import "librecode/workflow"
outcome, _ := workflow.Parallel([]any{3, 1, 2}, func(value any) (any, error) {
	return value.(int) * 10, nil
}, 2)
outcome`

	assertProfilesMatch(t, source, nil, func(t *testing.T, outcome workflowkernel.Outcome) {
		t.Helper()
		assert.Equal(t, workflowkernel.AggregateSuccess, outcome.State)
		assert.Equal(t, []any{30, 10, 20}, outcomeValues(outcome.Items))
		assert.Equal(t, 3, outcome.Counts[workflowkernel.StateCompleted])
	})
}

func TestCanonicalPipelineSemanticsMatchBothProfiles(t *testing.T) {
	t.Parallel()

	source := `import "librecode/workflow"
outcome, _ := workflow.Pipeline([]any{1, 2}, []func(any) (any, error){
	func(value any) (any, error) { return value.(int) + 1, nil },
	func(value any) (any, error) { return value.(int) * 3, nil },
}, 2)
outcome`

	assertProfilesMatch(t, source, nil, func(t *testing.T, outcome workflowkernel.Outcome) {
		t.Helper()
		assert.Equal(t, workflowkernel.AggregateSuccess, outcome.State)
		assert.Equal(t, []any{6, 9}, outcomeValues(outcome.Items))
	})
}

func TestCanonicalCombinatorsHandleEmptyInputInBothProfiles(t *testing.T) {
	t.Parallel()

	source := `import "librecode/workflow"
outcome, _ := workflow.Parallel([]any{}, func(value any) (any, error) { return value, nil }, 2)
outcome`

	assertProfilesMatch(t, source, nil, func(t *testing.T, outcome workflowkernel.Outcome) {
		t.Helper()
		assert.Equal(t, workflowkernel.AggregateSuccess, outcome.State)
		assert.Zero(t, outcome.Total)
		assert.Empty(t, outcome.Items)
	})
}

func TestCanonicalCombinatorsRetainPartialFailureInBothProfiles(t *testing.T) {
	t.Parallel()

	source := `import "librecode/workflow"
import "testsupport"
outcome, _ := workflow.Parallel([]any{1, 2, 3}, func(value any) (any, error) {
	return testsupport.FailSecond(value)
}, 1)
outcome`
	bindings := mvmhost.Bindings{"testsupport": {"FailSecond": func(value any) (any, error) {
		if value == 2 {
			return nil, errors.New("second failed")
		}

		return value, nil
	}}}

	assertProfilesMatch(t, source, bindings, func(t *testing.T, outcome workflowkernel.Outcome) {
		t.Helper()
		assert.Equal(t, workflowkernel.AggregatePartial, outcome.State)
		assert.Equal(t, workflowkernel.StateCompleted, outcome.Items[0].State)
		assert.Equal(t, workflowkernel.StateFailed, outcome.Items[1].State)
		assert.Equal(t, "second failed", outcome.Items[1].Error)
		assert.Equal(t, workflowkernel.StateNotStarted, outcome.Items[2].State)
	})
}

func TestCanonicalCombinatorsConvertPanicsInBothProfiles(t *testing.T) {
	t.Parallel()

	source := `import "librecode/workflow"
outcome, _ := workflow.Parallel([]any{1}, func(value any) (any, error) {
	panic("boom")
}, 1)
outcome`

	assertProfilesMatch(t, source, nil, func(t *testing.T, outcome workflowkernel.Outcome) {
		t.Helper()
		assert.Equal(t, workflowkernel.AggregateFailed, outcome.State)
		require.Len(t, outcome.Items, 1)
		assert.Equal(t, workflowkernel.StateFailed, outcome.Items[0].State)
		assert.Contains(t, outcome.Items[0].Error, "boom")
	})
}

func TestCanonicalCombinatorsHonorConcurrencyInBothProfiles(t *testing.T) {
	t.Parallel()

	for _, profile := range []guestapi.Profile{guestapi.ProfileTurn, guestapi.ProfileDurable} {
		t.Run(string(profile), func(t *testing.T) {
			t.Parallel()

			var (
				active, peak, arrived atomic.Int64
				watchdogFired         atomic.Bool
				release               sync.Once
			)

			barrier := make(chan struct{})
			watchdog := time.AfterFunc(time.Second, func() {
				watchdogFired.Store(true)
				release.Do(func() { close(barrier) })
			})

			defer watchdog.Stop()

			apply := func(value any) (any, error) {
				current := active.Add(1)
				defer active.Add(-1)

				updatePeak(&peak, current)

				if arrived.Add(1) == 2 {
					release.Do(func() { close(barrier) })
				}

				<-barrier

				return value, nil
			}
			bindings := mvmhost.Bindings{"testsupport": {"Apply": apply}}
			source := `import "librecode/workflow"
import "testsupport"
outcome, _ := workflow.Parallel([]any{1, 2, 3, 4}, func(value any) (any, error) {
	return testsupport.Apply(value)
}, 2)
outcome`
			outcome := evalCanonicalCombinator(t, profile, source, bindings)

			require.False(t, watchdogFired.Load(), "two workers did not reach the barrier")
			assert.Equal(t, workflowkernel.AggregateSuccess, outcome.State)
			assert.Equal(t, int64(2), peak.Load())
		})
	}
}

func assertProfilesMatch(
	t *testing.T,
	source string,
	extra mvmhost.Bindings,
	check func(*testing.T, workflowkernel.Outcome),
) {
	t.Helper()

	profiles := []guestapi.Profile{guestapi.ProfileTurn, guestapi.ProfileDurable}
	outcomes := make([]workflowkernel.Outcome, 0, len(profiles))

	for _, profile := range profiles {
		outcome := evalCanonicalCombinator(t, profile, source, extra)
		check(t, outcome)
		outcomes = append(outcomes, outcome)
	}

	assert.Equal(t, outcomes[0], outcomes[1], "turn and durable profiles must have identical semantics")
}

func evalCanonicalCombinator(
	t *testing.T,
	profile guestapi.Profile,
	source string,
	extra mvmhost.Bindings,
) workflowkernel.Outcome {
	t.Helper()

	bindings, err := executeworker.CompileBindings(profile, guestapi.CurrentVersion)
	require.NoError(t, err)

	maps.Copy(bindings, extra)

	result, err := mvmhost.New().Eval(t.Context(), mvmhost.Request{
		Bindings: bindings,
		Name:     "canonical-combinator.go",
		Source:   source,
	})
	require.NoError(t, err)

	outcome, ok := result.Value.(workflowkernel.Outcome)
	require.True(t, ok, "combinator result has type %T", result.Value)

	return outcome
}

func updatePeak(peak *atomic.Int64, current int64) {
	for {
		previous := peak.Load()
		if current <= previous || peak.CompareAndSwap(previous, current) {
			return
		}
	}
}

func outcomeValues(items []workflowkernel.ItemOutcome) []any {
	values := make([]any, len(items))
	for index := range items {
		values[index] = items[index].Value
	}

	return values
}
