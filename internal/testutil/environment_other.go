//go:build !windows

package testutil

import "testing"

func setWindowsHome(_ *testing.T, _ string) {}
