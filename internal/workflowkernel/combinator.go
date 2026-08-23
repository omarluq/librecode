// Package workflowkernel implements the database-independent bounded
// combinators exposed to MVM workflow programs.
package workflowkernel

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// ItemState is the lifecycle state of one input item.
type ItemState uint8

// Item lifecycle states.
const (
	StateNotStarted ItemState = iota
	StateQueued
	StateRunning
	StateCompleted
	StateFailed
	StateCanceled
	StateSkipped
)

// MarshalText supplies stable JSON strings for item states and count keys.
func (state ItemState) MarshalText() ([]byte, error) { return []byte(state.String()), nil }

// String returns the stable public name of an item state.
func (state ItemState) String() string {
	return [...]string{"not_started", "queued", "running", "completed", "failed", "canceled", "skipped"}[state]
}

// AggregateState summarizes terminal item outcomes.
type AggregateState uint8

// Aggregate terminal states.
const (
	AggregateSuccess AggregateState = iota
	AggregatePartial
	AggregateFailed
)

// MarshalText supplies stable JSON strings for aggregate states.
func (state AggregateState) MarshalText() ([]byte, error) { return []byte(state.String()), nil }

// String returns the stable public name of an aggregate state.
func (state AggregateState) String() string {
	return [...]string{"success", "partial", "failed"}[state]
}

// Callback is one parallel operation or pipeline stage.
type Callback func(any) (any, error)

// ItemOutcome is one terminal callback outcome. Stage is present only when a
// pipeline stage failed or was canceled. Outcomes retain input order.
type ItemOutcome struct {
	Value any       `json:"value"`
	Error string    `json:"error"`
	Index int       `json:"index"`
	Stage int       `json:"stage"`
	State ItemState `json:"state"`
}

// Outcome is the structured terminal result of a combinator invocation.
type Outcome struct {
	Counts map[ItemState]int `json:"counts"`
	Items  []ItemOutcome     `json:"items"`
	Total  int               `json:"total"`
	State  AggregateState    `json:"state"`
}

// Parallel applies callback to each item with bounded admission.
func Parallel(ctx context.Context, items []any, callback Callback, concurrency int) (Outcome, error) {
	if callback == nil {
		return Outcome{}, errors.New("parallel callback is required")
	}

	return run(ctx, items, []Callback{callback}, concurrency)
}

// Pipeline applies every stage sequentially to each admitted item while
// processing distinct items concurrently.
func Pipeline(ctx context.Context, items []any, stages []Callback, concurrency int) (Outcome, error) {
	for index, stage := range stages {
		if stage == nil {
			return Outcome{}, fmt.Errorf("pipeline stage %d is required", index)
		}
	}

	return run(ctx, items, stages, concurrency)
}

func run(ctx context.Context, items []any, stages []Callback, concurrency int) (Outcome, error) {
	if ctx == nil {
		return Outcome{}, errors.New("workflow combinator context is required")
	}

	if concurrency <= 0 {
		return Outcome{}, errors.New("workflow concurrency must be positive")
	}

	outcome := newOutcome(len(items))
	if len(items) == 0 {
		outcome.finish()

		return outcome, nil
	}

	state := admissionState{Mutex: sync.Mutex{}, next: 0, stopped: false}

	var workers sync.WaitGroup

	workers.Add(min(concurrency, len(items)))

	for range min(concurrency, len(items)) {
		go func() {
			defer workers.Done()

			for {
				index, admitted := state.admit(ctx, len(items))
				if !admitted {
					return
				}

				result := executeItem(ctx, index, items[index], stages)

				outcome.Items[index] = result
				if result.State == StateFailed {
					state.stop()
				}
			}
		}()
	}

	workers.Wait()

	// Cancellation and systemic callback failure leave every item which did
	// not cross the admission boundary explicitly distinguishable.
	outcome.finish()

	return outcome, nil
}

type admissionState struct {
	sync.Mutex
	next    int
	stopped bool
}

func (state *admissionState) admit(ctx context.Context, total int) (int, bool) {
	state.Lock()
	defer state.Unlock()

	if state.stopped || ctx.Err() != nil || state.next >= total {
		return 0, false
	}

	index := state.next
	state.next++

	return index, true
}

func (state *admissionState) stop() {
	state.Lock()
	state.stopped = true
	state.Unlock()
}

func executeItem(ctx context.Context, index int, value any, stages []Callback) ItemOutcome {
	result := ItemOutcome{Value: value, Error: "", Index: index, Stage: -1, State: StateRunning}

	for stageIndex, stage := range stages {
		if err := ctx.Err(); err != nil {
			result.State, result.Value, result.Error = StateCanceled, nil, err.Error()
			result.Stage = stageIndex

			return result
		}

		next, err := call(stage, result.Value)
		if err != nil {
			result.Value, result.Error = next, err.Error()
			result.Stage = stageIndex

			if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				result.State = StateCanceled
			} else {
				result.State = StateFailed
			}

			return result
		}

		result.Value = next
	}

	if err := ctx.Err(); err != nil {
		result.State, result.Value, result.Error = StateCanceled, nil, err.Error()
		if len(stages) > 0 {
			result.Stage = len(stages) - 1
		}

		return result
	}

	result.State = StateCompleted

	return result
}

func call(callback Callback, value any) (result any, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("callback panic: %v", recovered)
		}
	}()

	return callback(value)
}

func newOutcome(total int) Outcome {
	items := make([]ItemOutcome, total)
	for index := range items {
		items[index] = ItemOutcome{Value: nil, Error: "", Index: index, Stage: -1, State: StateNotStarted}
	}

	return Outcome{Counts: nil, Items: items, Total: total, State: AggregateFailed}
}

func (outcome *Outcome) finish() {
	counts := map[ItemState]int{
		StateNotStarted: 0, StateQueued: 0, StateRunning: 0, StateCompleted: 0,
		StateFailed: 0, StateCanceled: 0, StateSkipped: 0,
	}
	for _, item := range outcome.Items {
		counts[item.State]++
	}

	outcome.Counts = counts

	completed := counts[StateCompleted]
	switch {
	case completed == outcome.Total:
		outcome.State = AggregateSuccess
	case completed > 0:
		outcome.State = AggregatePartial
	default:
		outcome.State = AggregateFailed
	}
}
