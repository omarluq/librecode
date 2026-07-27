package assistant

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSessionOperationCoordinatorSerializesSameSession(t *testing.T) {
	t.Parallel()

	coordinator := newSessionOperationCoordinator()
	releaseFirst, err := coordinator.acquire(context.Background(), "session")
	require.NoError(t, err)

	type acquisition struct {
		release func()
		err     error
	}

	acquired := make(chan acquisition, 1)

	go func() {
		release, acquireErr := coordinator.acquire(context.Background(), "session")
		acquired <- acquisition{release: release, err: acquireErr}
	}()

	require.Eventually(t, func() bool {
		coordinator.mu.Lock()
		defer coordinator.mu.Unlock()

		return coordinator.slots["session"].refs == 2
	}, time.Second, time.Millisecond)

	select {
	case result := <-acquired:
		require.NoError(t, result.err)
		require.FailNow(t, "second operation acquired the session before the first released it")
	default:
	}

	releaseFirst()

	select {
	case result := <-acquired:
		require.NoError(t, result.err)
		require.NotNil(t, result.release)
		result.release()
	case <-time.After(time.Second):
		require.FailNow(t, "second operation did not acquire the released session")
	}
}

func TestSessionOperationCoordinatorAllowsDifferentSessions(t *testing.T) {
	t.Parallel()

	coordinator := newSessionOperationCoordinator()
	releaseFirst, err := coordinator.acquire(context.Background(), "first")
	require.NoError(t, err)

	defer releaseFirst()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	releaseSecond, err := coordinator.acquire(ctx, "second")
	require.NoError(t, err)
	releaseSecond()
}

func TestSessionOperationCoordinatorCancellation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		cancelBefore bool
	}{
		{name: "already canceled", cancelBefore: true},
		{name: "canceled while waiting", cancelBefore: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			coordinator := newSessionOperationCoordinator()
			releaseOwner, err := coordinator.acquire(context.Background(), "session")
			require.NoError(t, err)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			if testCase.cancelBefore {
				cancel()
			}

			type acquisition struct {
				release func()
				err     error
			}

			result := make(chan acquisition, 1)

			go func() {
				release, acquireErr := coordinator.acquire(ctx, "session")
				result <- acquisition{release: release, err: acquireErr}
			}()

			if !testCase.cancelBefore {
				cancel()
			}

			acquired := <-result
			require.ErrorIs(t, acquired.err, context.Canceled)
			require.Nil(t, acquired.release)

			releaseOwner()

			releaseNext, err := coordinator.acquire(context.Background(), "session")
			require.NoError(t, err)
			releaseNext()
		})
	}
}

func TestSessionOperationCoordinatorReleaseIsIdempotent(t *testing.T) {
	t.Parallel()

	coordinator := newSessionOperationCoordinator()
	release, err := coordinator.acquire(context.Background(), "session")
	require.NoError(t, err)

	release()
	release()

	releaseNext, err := coordinator.acquire(context.Background(), "session")
	require.NoError(t, err)
	releaseNext()
}
