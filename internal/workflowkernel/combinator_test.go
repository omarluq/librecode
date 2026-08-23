package workflowkernel_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/workflowkernel"
)

func TestParallelPreservesOrderAndBoundsAdmission(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	started := make(chan struct{}, 4)

	var active, peak atomic.Int64

	callback := func(value any) (any, error) {
		integer, ok := value.(int)
		if !ok {
			return nil, errors.New("item is not an integer")
		}

		current := active.Add(1)

		for {
			previous := peak.Load()
			if current <= previous || peak.CompareAndSwap(previous, current) {
				break
			}
		}

		started <- struct{}{}

		<-release
		active.Add(-1)

		return integer * 2, nil
	}

	type parallelResult struct {
		err     error
		outcome workflowkernel.Outcome
	}

	resultChannel := make(chan parallelResult, 1)

	go func() {
		outcome, err := workflowkernel.Parallel(context.Background(), []any{1, 2, 3, 4}, callback, 2)

		resultChannel <- parallelResult{outcome: outcome, err: err}
	}()

	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for admitted callbacks")
		}
	}

	close(release)

	result := <-resultChannel
	require.NoError(t, result.err)
	outcome := result.outcome

	assert.Equal(t, int64(2), peak.Load())
	assert.Equal(t, workflowkernel.AggregateSuccess, outcome.State)
	assert.Equal(t, []any{2, 4, 6, 8}, values(outcome.Items))
	assert.Equal(t, 4, outcome.Counts[workflowkernel.StateCompleted])
}

func TestParallelFailureStopsAdmissionAndMarksNotStarted(t *testing.T) {
	t.Parallel()

	outcome, err := workflowkernel.Parallel(context.Background(), []any{0, 1, 2}, func(_ any) (any, error) {
		return nil, errors.New("systemic")
	}, 1)
	require.NoError(t, err)

	assert.Equal(t, workflowkernel.StateFailed, outcome.Items[0].State)
	assert.Equal(t, workflowkernel.StateNotStarted, outcome.Items[1].State)
	assert.Equal(t, workflowkernel.StateNotStarted, outcome.Items[2].State)
	assert.Equal(t, workflowkernel.AggregateFailed, outcome.State)
}

func TestPipelineStagesAreSequentialAndPanicIsStructured(t *testing.T) {
	t.Parallel()

	outcome, err := workflowkernel.Pipeline(context.Background(), []any{1, 2}, []workflowkernel.Callback{
		func(value any) (any, error) {
			integer, ok := value.(int)
			if !ok {
				return nil, errors.New("item is not an integer")
			}

			return integer + 1, nil
		},
		func(value any) (any, error) {
			integer, ok := value.(int)
			if !ok {
				return nil, errors.New("item is not an integer")
			}

			if integer == 3 {
				panic("boom")
			}

			return integer * 10, nil
		},
	}, 1)
	require.NoError(t, err)

	assert.Equal(t, 20, outcome.Items[0].Value)
	assert.Equal(t, workflowkernel.StateCompleted, outcome.Items[0].State)
	assert.Equal(t, workflowkernel.StateFailed, outcome.Items[1].State)
	assert.Equal(t, "callback panic: boom", outcome.Items[1].Error)

	assert.Equal(t, 1, outcome.Items[1].Stage)
}

func TestCombinatorsCancellationAndEmptyInput(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	outcome, err := workflowkernel.Parallel(ctx, []any{1, 2}, func(value any) (any, error) { return value, nil }, 1)
	require.NoError(t, err)
	assert.Equal(t, workflowkernel.AggregateFailed, outcome.State)
	assert.Equal(t, 2, outcome.Counts[workflowkernel.StateNotStarted])

	ctx, cancel = context.WithCancel(context.Background())
	outcome, err = workflowkernel.Parallel(ctx, []any{1, 2}, func(value any) (any, error) {
		cancel()

		return value, nil
	}, 1)
	require.NoError(t, err)
	assert.Equal(t, workflowkernel.StateCanceled, outcome.Items[0].State)
	assert.Equal(t, workflowkernel.StateNotStarted, outcome.Items[1].State)

	empty, err := workflowkernel.Pipeline(context.Background(), nil, nil, 3)
	require.NoError(t, err)
	assert.Equal(t, workflowkernel.AggregateSuccess, empty.State)
	assert.Zero(t, empty.Total)
	assert.Empty(t, empty.Items)
}

func TestCombinatorValidation(t *testing.T) {
	t.Parallel()

	_, err := workflowkernel.Parallel(context.Background(), nil, nil, 1)
	require.EqualError(t, err, "parallel callback is required")
	_, err = workflowkernel.Pipeline(context.Background(), nil, []workflowkernel.Callback{nil}, 1)
	require.EqualError(t, err, "pipeline stage 0 is required")
	_, err = workflowkernel.Pipeline(context.Background(), nil, nil, 0)
	require.EqualError(t, err, "workflow concurrency must be positive")
}

func values(items []workflowkernel.ItemOutcome) []any {
	result := make([]any, len(items))
	for index := range items {
		result[index] = items[index].Value
	}

	return result
}
