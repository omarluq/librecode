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

	acquired := make(chan func(), 1)

	go func() {
		release, acquireErr := coordinator.acquire(context.Background(), "session")
		if acquireErr == nil {
			acquired <- release
		}
	}()

	select {
	case <-acquired:
		t.Fatal("second operation acquired the session before the first released it")
	case <-time.After(25 * time.Millisecond):
	}

	releaseFirst()

	select {
	case releaseSecond := <-acquired:
		releaseSecond()
	case <-time.After(time.Second):
		t.Fatal("second operation did not acquire the released session")
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

func TestSessionOperationCoordinatorCancelsWaiter(t *testing.T) {
	t.Parallel()

	coordinator := newSessionOperationCoordinator()
	releaseOwner, err := coordinator.acquire(context.Background(), "session")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	releaseWaiter, err := coordinator.acquire(ctx, "session")
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, releaseWaiter)

	releaseOwner()

	releaseNext, err := coordinator.acquire(context.Background(), "session")
	require.NoError(t, err)
	releaseNext()
}
