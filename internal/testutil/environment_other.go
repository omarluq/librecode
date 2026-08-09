//go:build !windows

package testutil

import "testing"

func setWindowsHome(_ *testing.T, _ string) {
	// Non-Windows platforms do not use the Windows-specific home variables.
}
