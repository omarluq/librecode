package assistant

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testSteeringSession = "session"
	testSteeringRun     = "run"
	testSteeringMessage = "message"
)

func TestSteeringInboxRegistryFIFOAndDefensiveCopy(t *testing.T) {
	t.Parallel()

	registry := newSteeringInboxRegistry(2)
	require.NoError(t, registry.register("session", "run"))

	data := []byte{1, 2, 3}
	require.NoError(t, registry.accept("session", "run", steeringDraft{
		Text: "first",
		Images: []ImageAttachment{{
			Name: "image.png", MIMEType: imageMIMEPNG, Data: data, Width: 1, Height: 1,
		}},
		HideUserPrompt: false,
	}))
	require.NoError(t, registry.accept("session", "run", newTestSteeringDraft("second")))

	data[0] = 9

	drafts, err := registry.drain("session", "run")
	require.NoError(t, err)
	require.Len(t, drafts, 2)
	assert.Equal(t, []string{"first", "second"}, []string{drafts[0].Text, drafts[1].Text})
	assert.Equal(t, byte(1), drafts[0].Images[0].Data[0])
}

func TestSteeringInboxRegistryErrors(t *testing.T) {
	t.Parallel()

	registry := newSteeringInboxRegistry(1)
	require.ErrorIs(t, registry.accept("missing", "run", newTestSteeringDraft("x")), ErrSteeringInactive)
	require.NoError(t, registry.register("session", "run"))
	require.ErrorIs(t, registry.register("session", "other"), ErrSteeringStaleRun)
	require.ErrorIs(t, registry.accept("session", "other", newTestSteeringDraft("x")), ErrSteeringStaleRun)
	require.NoError(t, registry.accept("session", "run", newTestSteeringDraft("x")))
	require.ErrorIs(t, registry.accept("session", "run", newTestSteeringDraft("y")), ErrSteeringCapacity)

	drafts, err := registry.close("session", "run")
	require.NoError(t, err)
	require.Len(t, drafts, 1)
	assert.ErrorIs(t, registry.accept("session", "run", newTestSteeringDraft("z")), ErrSteeringInactive)
}

func TestSteeringInboxRegistryFinalDrainSettlesEmptyInbox(t *testing.T) {
	t.Parallel()

	registry := newSteeringInboxRegistry(2)
	require.NoError(t, registry.register("session", "run"))

	drafts, err := registry.drainFinal("session", "run")
	require.NoError(t, err)
	assert.Empty(t, drafts)
	assert.ErrorIs(t, registry.accept("session", "run", newTestSteeringDraft("late")), ErrSteeringInactive)
}

func TestSteeringInboxRegistryFinalDrainKeepsPendingInboxActive(t *testing.T) {
	t.Parallel()

	registry := newSteeringInboxRegistry(2)
	require.NoError(t, registry.register("session", "run"))
	require.NoError(t, registry.accept("session", "run", newTestSteeringDraft("accepted")))

	drafts, err := registry.drainFinal("session", "run")
	require.NoError(t, err)
	require.Len(t, drafts, 1)
	assert.Equal(t, "accepted", drafts[0].Text)

	require.NoError(t, registry.accept("session", "run", newTestSteeringDraft("next boundary")))
	restored, err := registry.close("session", "run")
	require.NoError(t, err)
	require.Len(t, restored, 1)
	assert.Equal(t, "next boundary", restored[0].Text)
}

func TestSteeringInboxRegistryConcurrentAcceptAndClose(t *testing.T) {
	t.Parallel()

	registry := newSteeringInboxRegistry(128)
	require.NoError(t, registry.register("session", "run"))

	var wait sync.WaitGroup

	errorsFound := make(chan error, 128)

	for index := range 128 {
		wait.Go(func() {
			errorsFound <- registry.accept(testSteeringSession, testSteeringRun, steeringDraft{
				Text: testSteeringMessage, Images: nil, HideUserPrompt: false,
			})
		})

		if index == 64 {
			wait.Go(func() {
				_, closeErr := registry.close(testSteeringSession, testSteeringRun)
				if closeErr != nil {
					errorsFound <- closeErr
				}
			})
		}
	}

	wait.Wait()
	close(errorsFound)

	for err := range errorsFound {
		if err == nil {
			continue
		}

		assert.True(t, errors.Is(err, ErrSteeringInactive) || errors.Is(err, ErrSteeringClosed))
	}
}

func TestRuntimeSteerValidation(t *testing.T) {
	t.Parallel()

	runtime := NewRuntimeForTest(nil)
	err := runtime.Steer(context.Background(), newTestSteeringRequest(
		testSteeringSession, testSteeringRun, testSteeringMessage,
	))
	require.ErrorIs(t, err, ErrSteeringInactive)

	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	err = runtime.Steer(canceled, newTestSteeringRequest(
		testSteeringSession, testSteeringRun, testSteeringMessage,
	))
	assert.ErrorIs(t, err, context.Canceled)
}

func TestRuntimeSteerInvalidInputSentinel(t *testing.T) {
	t.Parallel()

	runtime := NewRuntimeForTest(nil)
	require.NoError(t, runtime.steering.register("session", "run"))

	tests := []struct {
		request *SteeringRequest
		name    string
	}{
		{request: nil, name: "nil request"},
		{
			request: newTestSteeringRequest("", testSteeringRun, testSteeringMessage),
			name:    "missing session",
		},
		{
			request: newTestSteeringRequest(testSteeringSession, "", testSteeringMessage),
			name:    "missing run",
		},
		{
			request: newTestSteeringRequest(testSteeringSession, testSteeringRun, ""),
			name:    "empty content",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := runtime.Steer(context.Background(), testCase.request)
			require.ErrorIs(t, err, ErrSteeringInvalidInput)
		})
	}
}

func newTestSteeringDraft(text string) steeringDraft {
	return steeringDraft{Text: text, Images: nil, HideUserPrompt: false}
}

func newTestSteeringRequest(sessionID, runID, text string) *SteeringRequest {
	return &SteeringRequest{
		SessionID: sessionID, RunID: runID, Text: text, Images: nil, HideUserPrompt: false,
	}
}
