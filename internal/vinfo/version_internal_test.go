package vinfo

import (
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	testBuildInfoTimeKey = "vcs.time"
	testBuildDate        = "2026-06-24T00:00:00Z"
	testCommit           = "abc1234"
	testDirtyVersion     = testShortRevision + dirtySuffix
	testFullRevision     = "25e59c5c54787d963bda41fe594517598334ff27"
	testShortRevision    = "25e59c5c"
	testUpdatedBuildDate = "2026-06-25T00:00:00Z"
	testVersion          = "1.2.3"
	testVersionExpected  = testVersion + " (commit=" + testCommit + ", built=" + testBuildDate + ")"
)

func TestStringUsesInjectedBuildMetadata(t *testing.T) {
	t.Parallel()

	assert.Equal(
		t,
		testVersionExpected,
		stringFromVersion(testVersion+metadataSeparator+testCommit+metadataSeparator+testBuildDate, nil),
	)
}

func TestParseBuildMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		expected buildMetadata
		name     string
		value    string
	}{
		{
			name:  "plain version",
			value: testVersion,
			expected: buildMetadata{
				version:   testVersion,
				commit:    defaultCommit,
				buildDate: defaultBuildDate,
			},
		},
		{
			name:  "full metadata",
			value: " " + testVersion + " | " + testCommit + " | " + testBuildDate + " ",
			expected: buildMetadata{
				version:   testVersion,
				commit:    testCommit,
				buildDate: testBuildDate,
			},
		},
		{
			name:  "missing metadata fields use defaults",
			value: "| |",
			expected: buildMetadata{
				version:   devVersion,
				commit:    defaultCommit,
				buildDate: defaultBuildDate,
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.expected, parseBuildMetadata(testCase.value))
		})
	}
}

func TestBuildMetadataWithBuildInfoFallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		main     debug.Module
		input    buildMetadata
		expected buildMetadata
		name     string
		settings []debug.BuildSetting
	}{
		{
			input: buildMetadata{
				version:   devVersion,
				commit:    defaultCommit,
				buildDate: defaultBuildDate,
			},
			expected: buildMetadata{
				version:   testDirtyVersion,
				commit:    testShortRevision,
				buildDate: defaultBuildDate,
			},
			main: debug.Module{},
			settings: []debug.BuildSetting{
				{Key: buildInfoRevisionKey, Value: testFullRevision},
				{Key: buildInfoModifiedKey, Value: trueValue},
				{Key: testBuildInfoTimeKey, Value: testBuildDate},
			},
			name: "vcs revision fallback",
		},
		{
			input: buildMetadata{
				version:   devVersion,
				commit:    defaultCommit,
				buildDate: defaultBuildDate,
			},
			expected: buildMetadata{
				version:   testVersion,
				commit:    testShortRevision,
				buildDate: defaultBuildDate,
			},
			main: debug.Module{Version: testVersion},
			settings: []debug.BuildSetting{
				{Key: buildInfoRevisionKey, Value: testFullRevision},
				{Key: buildInfoModifiedKey, Value: trueValue},
				{Key: testBuildInfoTimeKey, Value: testBuildDate},
			},
			name: "module version fallback",
		},
		{
			input: buildMetadata{
				version:   testVersion,
				commit:    testCommit,
				buildDate: testBuildDate,
			},
			expected: buildMetadata{
				version:   testVersion,
				commit:    testCommit,
				buildDate: testBuildDate,
			},
			main: debug.Module{},
			settings: []debug.BuildSetting{
				{Key: buildInfoRevisionKey, Value: testFullRevision},
				{Key: buildInfoModifiedKey, Value: trueValue},
				{Key: testBuildInfoTimeKey, Value: testUpdatedBuildDate},
			},
			name: "injected values preserved",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			metadata := testCase.input.withBuildInfoFallback(&debug.BuildInfo{
				Main:     testCase.main,
				Settings: testCase.settings,
			})

			assert.Equal(t, testCase.expected, metadata)
		})
	}
}

func TestBuildInfoSetting(t *testing.T) {
	t.Parallel()

	settings := []debug.BuildSetting{{Key: buildInfoRevisionKey, Value: "  " + testCommit + "  "}}

	assert.Equal(t, testCommit, buildInfoSetting(settings, buildInfoRevisionKey))
	assert.Empty(t, buildInfoSetting(settings, "missing"))
}

func TestShortRevision(t *testing.T) {
	t.Parallel()

	assert.Equal(t, testShortRevision, shortRevision(testFullRevision))
	assert.Equal(t, testCommit, shortRevision(" "+testCommit+" "))
}

func TestVersionPart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		expected string
		name     string
		version  string
	}{
		{name: "empty", version: "", expected: devVersion},
		{name: "devel", version: "(devel)", expected: devVersion},
		{name: "trimmed", version: "  v1.0.0  ", expected: "v1.0.0"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.expected, versionPart(testCase.version))
		})
	}
}
