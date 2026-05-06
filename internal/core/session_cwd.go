package core

// SessionCWDSource exposes session working-directory metadata.
type SessionCWDSource interface {
	CWD() string
	SessionFile() string
}

// SessionCWDIssue describes a missing stored session working directory.
type SessionCWDIssue struct {
	SessionFile string `json:"session_file,omitempty"`
	SessionCWD  string `json:"session_cwd"`
	FallbackCWD string `json:"fallback_cwd"`
}

// MissingSessionCWDError reports a missing stored working directory.
type MissingSessionCWDError struct {
	Issue SessionCWDIssue `json:"issue"`
}

// Error formats the missing cwd issue.
func (err *MissingSessionCWDError) Error() string {
	return FormatMissingSessionCWDError(err.Issue)
}

// MissingSessionCWDIssueFor returns an issue if the session file points to a missing cwd.
func MissingSessionCWDIssueFor(source SessionCWDSource, fallbackCWD string) (SessionCWDIssue, bool) {
	sessionFile := source.SessionFile()
	if sessionFile == "" {
		return SessionCWDIssue{SessionFile: "", SessionCWD: "", FallbackCWD: ""}, false
	}
	sessionCWD := source.CWD()
	if sessionCWD == "" || resourcePathExists(sessionCWD) {
		return SessionCWDIssue{SessionFile: "", SessionCWD: "", FallbackCWD: ""}, false
	}

	return SessionCWDIssue{SessionFile: sessionFile, SessionCWD: sessionCWD, FallbackCWD: fallbackCWD}, true
}

// FormatMissingSessionCWDError formats a terminal error message.
func FormatMissingSessionCWDError(issue SessionCWDIssue) string {
	sessionFile := ""
	if issue.SessionFile != "" {
		sessionFile = "\nSession file: " + issue.SessionFile
	}

	return "Stored session working directory does not exist: " + issue.SessionCWD +
		sessionFile + "\nCurrent working directory: " + issue.FallbackCWD
}

// FormatMissingSessionCWDPrompt formats an interactive confirmation prompt.
func FormatMissingSessionCWDPrompt(issue SessionCWDIssue) string {
	return "cwd from session file does not exist\n" + issue.SessionCWD +
		"\n\ncontinue in current cwd\n" + issue.FallbackCWD
}

// AssertSessionCWDExists returns MissingSessionCWDError when the stored cwd is missing.
func AssertSessionCWDExists(source SessionCWDSource, fallbackCWD string) error {
	issue, found := MissingSessionCWDIssueFor(source, fallbackCWD)
	if !found {
		return nil
	}

	return &MissingSessionCWDError{Issue: issue}
}
