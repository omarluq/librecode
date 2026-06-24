// Package vinfo exposes build metadata for CLI version reporting.
package vinfo

import (
	"fmt"
	"runtime/debug"
	"strings"
)

const (
	buildInfoModifiedKey = "vcs.modified"
	buildInfoRevisionKey = "vcs.revision"
	buildInfoTimeKey     = "vcs.time"
	devVersion           = "dev"
	dirtySuffix          = "-dirty"
	defaultCommit        = "none"
	defaultBuildDate     = "unknown"
	metadataSeparator    = "|"
	shortRevisionBytes   = 8
	trueValue            = "true"
)

// version is set by release/build -ldflags as "version|commit|buildDate".
var version = devVersion

type buildMetadata struct {
	version   string
	commit    string
	buildDate string
}

// String returns a human-readable build version string.
func String() string {
	var info *debug.BuildInfo
	if buildInfo, ok := debug.ReadBuildInfo(); ok {
		info = buildInfo
	}

	return stringFromVersion(version, info)
}

func stringFromVersion(value string, info *debug.BuildInfo) string {
	metadata := parseBuildMetadata(value)
	if info != nil {
		metadata = metadata.withBuildInfoFallback(info)
	}

	return metadata.String()
}

func (metadata buildMetadata) String() string {
	return fmt.Sprintf("%s (commit=%s, built=%s)", metadata.version, metadata.commit, metadata.buildDate)
}

func parseBuildMetadata(value string) buildMetadata {
	versionValue, rest, hasMetadata := strings.Cut(value, metadataSeparator)

	metadata := buildMetadata{
		version:   versionPart(versionValue),
		commit:    defaultCommit,
		buildDate: defaultBuildDate,
	}
	if !hasMetadata {
		return metadata
	}

	commitValue, buildDateValue, hasBuildDate := strings.Cut(rest, metadataSeparator)

	metadata.commit = metadataPart(commitValue, defaultCommit)
	if hasBuildDate {
		metadata.buildDate = metadataPart(buildDateValue, defaultBuildDate)
	}

	return metadata
}

func (metadata buildMetadata) withBuildInfoFallback(info *debug.BuildInfo) buildMetadata {
	if metadata.version == devVersion {
		metadata.version = buildInfoVersion(info)
	}

	if metadata.commit == defaultCommit {
		revision := shortRevision(buildInfoSetting(info.Settings, buildInfoRevisionKey))
		metadata.commit = metadataPart(revision, defaultCommit)
	}

	if metadata.buildDate == defaultBuildDate {
		metadata.buildDate = metadataPart(buildInfoSetting(info.Settings, buildInfoTimeKey), defaultBuildDate)
	}

	return metadata
}

func buildInfoVersion(info *debug.BuildInfo) string {
	moduleVersion := versionPart(info.Main.Version)
	if moduleVersion != devVersion {
		return moduleVersion
	}

	return fallbackVCSVersion(info.Settings)
}

func fallbackVCSVersion(settings []debug.BuildSetting) string {
	revision := shortRevision(buildInfoSetting(settings, buildInfoRevisionKey))
	if revision == "" {
		return devVersion
	}

	if buildInfoSetting(settings, buildInfoModifiedKey) == trueValue {
		return revision + dirtySuffix
	}

	return revision
}

func buildInfoSetting(settings []debug.BuildSetting, key string) string {
	for _, setting := range settings {
		if setting.Key == key {
			return strings.TrimSpace(setting.Value)
		}
	}

	return ""
}

func shortRevision(revision string) string {
	revision = strings.TrimSpace(revision)
	if len(revision) <= shortRevisionBytes {
		return revision
	}

	return revision[:shortRevisionBytes]
}

func metadataPart(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}

	return value
}

func versionPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "(devel)" {
		return devVersion
	}

	return value
}
