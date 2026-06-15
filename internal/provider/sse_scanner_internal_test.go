package provider

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScanSSEEventsPreservesEventNameAndMultilineData(t *testing.T) {
	t.Parallel()

	var events []sseEvent

	stream := strings.Join([]string{
		"event: message",
		"data: one",
		"data: two",
		"",
		"data: " + sseDoneData,
		"",
	}, "\n")
	err := scanSSEEvents(strings.NewReader(stream), func(event sseEvent) error {
		events = append(events, event)

		return nil
	})

	require.NoError(t, err)
	require.Len(t, events, 2)
	assert.Equal(t, "message", events[0].Name)
	assert.Equal(t, "one\ntwo", events[0].Data)
	assert.Equal(t, sseDoneData, events[1].Data)
}

func TestScanSSEEventsReportsReadErrors(t *testing.T) {
	t.Parallel()

	err := scanSSEEvents(errorReader{}, func(sseEvent) error { return nil })

	require.Error(t, err)
	assert.Contains(t, err.Error(), "read provider stream")
}

func TestScanSSEEventsPropagatesHandlerErrors(t *testing.T) {
	t.Parallel()

	want := errors.New("stop")
	err := scanSSEEvents(strings.NewReader("data: {}\n\n"), func(sseEvent) error { return want })

	require.ErrorIs(t, err, want)
}

var _ io.Reader = errorReader{}
