package core

import (
	"fmt"
	"strings"
	"time"
)

// TimingEntry records one elapsed startup segment.
type TimingEntry struct {
	Label string        `json:"label"`
	Delta time.Duration `json:"delta"`
}

// Timings records opt-in startup timing instrumentation.
type Timings struct {
	last    time.Time
	clock   func() time.Time
	entries []TimingEntry
	enabled bool
}

// NewTimings creates a timing recorder.
func NewTimings(enabled bool) *Timings {
	return NewTimingsWithClock(enabled, time.Now)
}

// NewTimingsWithClock creates a timing recorder with an injected clock.
func NewTimingsWithClock(enabled bool, clock func() time.Time) *Timings {
	now := clock()

	return &Timings{entries: []TimingEntry{}, clock: clock, last: now, enabled: enabled}
}

// Reset clears timings and restarts the clock.
func (timings *Timings) Reset() {
	if !timings.enabled {
		return
	}
	timings.entries = []TimingEntry{}
	timings.last = timings.clock()
}

// Mark records elapsed time since the previous mark.
func (timings *Timings) Mark(label string) {
	if !timings.enabled {
		return
	}
	now := timings.clock()
	timings.entries = append(timings.entries, TimingEntry{Label: label, Delta: now.Sub(timings.last)})
	timings.last = now
}

// Entries returns recorded timings.
func (timings *Timings) Entries() []TimingEntry {
	return append([]TimingEntry{}, timings.entries...)
}

// String formats timings for stderr or logs.
func (timings *Timings) String() string {
	if !timings.enabled || len(timings.entries) == 0 {
		return ""
	}
	lines := []string{"--- Startup Timings ---"}
	total := time.Duration(0)
	for _, entry := range timings.entries {
		total += entry.Delta
		lines = append(lines, fmt.Sprintf("  %s: %dms", entry.Label, entry.Delta.Milliseconds()))
	}
	lines = append(lines, fmt.Sprintf("  TOTAL: %dms", total.Milliseconds()), "------------------------")

	return strings.Join(lines, "\n")
}
