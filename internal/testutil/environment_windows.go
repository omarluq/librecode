//go:build windows

package testutil

import (
	"path/filepath"
	"testing"
)

func setWindowsHome(t *testing.T, home string) {
	t.Helper()

	t.Setenv("USERPROFILE", home)
	volume := filepath.VolumeName(home)
	t.Setenv("HOMEDRIVE", volume)
	t.Setenv("HOMEPATH", home[len(volume):])
}
