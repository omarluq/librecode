// Package vinfo exposes build metadata for CLI version reporting.
package vinfo

import (
	"fmt"
	"runtime/debug"
	"strings"
)

const devVersion = "dev"

var (
	// Version is the semantic version injected at build time.
	Version = devVersion
	// Commit is the VCS revision injected at build time.
	Commit = "none"
	// BuildDate is the UTC build timestamp injected at build time.
	BuildDate = "unknown"
)

// String returns a human-readable build version string.
func String() string {
	version := Version

	if version == devVersion {
		if info, ok := debug.ReadBuildInfo(); ok {
			version = fallbackVersion(info.Main.Version)
		}
	}

	return fmt.Sprintf("%s (commit=%s, built=%s)", version, Commit, BuildDate)
}

func fallbackVersion(version string) string {
	if version == "" || version == "(devel)" {
		return devVersion
	}

	return strings.TrimSpace(version)
}
