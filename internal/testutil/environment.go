package testutil

import "testing"

// SetWindowsHome configures the environment variables used by
// os.UserHomeDir on Windows. It is a no-op on other platforms.
func SetWindowsHome(t *testing.T, home string) {
	t.Helper()
	setWindowsHome(t, home)
}
