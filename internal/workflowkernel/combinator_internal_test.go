package workflowkernel

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPublishedTerminalErrorPreventsSubsequentAdmission(t *testing.T) {
	t.Parallel()

	for _, state := range []ItemState{StateFailed, StateCanceled} {
		t.Run(state.String(), func(t *testing.T) {
			t.Parallel()

			outcome := newOutcome(2)
			admission := admissionState{Mutex: sync.Mutex{}, next: 0, stopped: false}

			index, admitted := admission.admit(context.Background(), 2)
			assert.True(t, admitted)

			admission.publish(&outcome, index, ItemOutcome{
				Value: nil, Error: "terminal", Index: index, Stage: 0, State: state,
			})

			_, admitted = admission.admit(context.Background(), 2)
			assert.False(t, admitted)
			assert.Equal(t, state, outcome.Items[index].State)
			assert.Equal(t, StateNotStarted, outcome.Items[1].State)
		})
	}
}
