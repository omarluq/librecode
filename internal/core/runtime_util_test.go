package core_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/core"
)

const testSessionFile = "session.jsonl"

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

func TestSessionCWDReportsMissingStoredDirectory(t *testing.T) {
	t.Parallel()

	fallbackCWD := t.TempDir()
	missingCWD := filepath.Join(t.TempDir(), "missing")
	issue, found := core.MissingSessionCWDIssueFor(testSessionCWDSource{
		cwd:         missingCWD,
		sessionFile: testSessionFile,
	}, fallbackCWD)
	require.True(t, found)

	assert.Equal(t, missingCWD, issue.SessionCWD)
	assert.Contains(t, core.FormatMissingSessionCWDError(issue), "Session file: "+testSessionFile)
	assert.Contains(t, core.FormatMissingSessionCWDPrompt(issue), fallbackCWD)

	var missingErr *core.MissingSessionCWDError
	require.ErrorAs(t, core.AssertSessionCWDExists(testSessionCWDSource{
		cwd:         missingCWD,
		sessionFile: testSessionFile,
	}, fallbackCWD), &missingErr)
	assert.Contains(t, missingErr.Error(), missingCWD)
	require.NoError(t, core.AssertSessionCWDExists(testSessionCWDSource{
		cwd:         fallbackCWD,
		sessionFile: testSessionFile,
	}, fallbackCWD))
	_, found = core.MissingSessionCWDIssueFor(testSessionCWDSource{
		cwd:         missingCWD,
		sessionFile: "",
	}, fallbackCWD)
	assert.False(t, found)
}

