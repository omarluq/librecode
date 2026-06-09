package core_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/core"
)

func TestTimingsDisabledDoesNotRecord(t *testing.T) {
	t.Parallel()

	current := time.Unix(0, 0)
	timings := core.NewTimingsWithClock(false, func() time.Time { return current })
	current = current.Add(time.Second)
	timings.Mark("ignored")
	timings.Reset()

	assert.Empty(t, timings.Entries())
	assert.Empty(t, timings.String())
}

func TestTimingsEntriesAreDefensiveCopy(t *testing.T) {
	t.Parallel()

	current := time.Unix(0, 0)
	timings := core.NewTimingsWithClock(true, func() time.Time { return current })
	current = current.Add(time.Second)
	timings.Mark("load")

	entries := timings.Entries()
	require.Len(t, entries, 1)
	entries[0].Label = "mutated"

	assert.Equal(t, "load", timings.Entries()[0].Label)
}

func TestTimingsResetRestartsClock(t *testing.T) {
	t.Parallel()

	current := time.Unix(0, 0)
	timings := core.NewTimingsWithClock(true, func() time.Time { return current })
	current = current.Add(time.Second)
	timings.Mark("before")
	timings.Reset()
	current = current.Add(250 * time.Millisecond)
	timings.Mark("after")

	entries := timings.Entries()
	require.Len(t, entries, 1)
	assert.Equal(t, "after", entries[0].Label)
	assert.Equal(t, 250*time.Millisecond, entries[0].Delta)
}
