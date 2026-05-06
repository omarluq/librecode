package core_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/core"
)

type testSessionCWDSource struct {
	cwd         string
	sessionFile string
}

func (source testSessionCWDSource) CWD() string {
	return source.cwd
}

func (source testSessionCWDSource) SessionFile() string {
	return source.sessionFile
}

type testTelemetrySettings struct {
	enabled bool
}

func (settings testTelemetrySettings) InstallTelemetryEnabled() bool {
	return settings.enabled
}

func TestSessionCWDReportsMissingStoredDirectory(t *testing.T) {
	t.Parallel()

	fallbackCWD := t.TempDir()
	missingCWD := filepath.Join(t.TempDir(), "missing")
	issue, found := core.MissingSessionCWDIssueFor(testSessionCWDSource{
		cwd:         missingCWD,
		sessionFile: "session.jsonl",
	}, fallbackCWD)
	require.True(t, found)

	assert.Equal(t, missingCWD, issue.SessionCWD)
	assert.Contains(t, core.FormatMissingSessionCWDError(issue), "Session file: session.jsonl")
	assert.Contains(t, core.FormatMissingSessionCWDPrompt(issue), fallbackCWD)
	var missingErr *core.MissingSessionCWDError
	assert.ErrorAs(t, core.AssertSessionCWDExists(testSessionCWDSource{
		cwd:         missingCWD,
		sessionFile: "session.jsonl",
	}, fallbackCWD), &missingErr)
}

func TestTelemetryEnvOverridesSettings(t *testing.T) {
	t.Parallel()

	assert.True(t, core.TruthyEnvFlag("YES"))
	assert.False(t, core.InstallTelemetryEnabled(testTelemetrySettings{enabled: true}, "0", true))
	assert.False(t, core.InstallTelemetryEnabled(testTelemetrySettings{enabled: false}, "", false))
}

func TestTimingsRecordsElapsedSegments(t *testing.T) {
	t.Parallel()

	current := time.Unix(0, 0)
	timings := core.NewTimingsWithClock(true, func() time.Time {
		return current
	})
	current = current.Add(25 * time.Millisecond)
	timings.Mark("load")
	current = current.Add(75 * time.Millisecond)
	timings.Mark("render")

	entries := timings.Entries()
	require.Len(t, entries, 2)
	assert.Equal(t, 25*time.Millisecond, entries[0].Delta)
	assert.Contains(t, timings.String(), "TOTAL: 100ms")
}
