// Package vinfo exposes build metadata for CLI version reporting.
package vinfo

import (
	"fmt"
	"runtime/debug"
	"strings"
)

const devVersion = "dev"

var version = devVersion

func commit() string {
	return "none"
}

func buildDate() string {
	return "unknown"
}

// String returns a human-readable build version string.
func String() string {
	value := version

	if value == devVersion {
		if info, ok := debug.ReadBuildInfo(); ok {
			value = fallbackVersion(info.Main.Version)
		}
	}

	return fmt.Sprintf("%s (commit=%s, built=%s)", value, commit(), buildDate())
}

func fallbackVersion(version string) string {
	if version == "" || version == "(devel)" {
		return devVersion
	}

	return strings.TrimSpace(version)
}
