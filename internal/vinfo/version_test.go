//nolint:testpackage // build metadata globals and fallback helper need direct branch coverage.
package vinfo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStringUsesInjectedBuildMetadata(t *testing.T) {
	t.Parallel()

	oldVersion, oldCommit, oldBuildDate := Version, Commit, BuildDate
	t.Cleanup(func() {
		Version, Commit, BuildDate = oldVersion, oldCommit, oldBuildDate
	})
	Version = "1.2.3"
	Commit = "abc123"
	BuildDate = "2026-06-09T00:00:00Z"

	assert.Equal(t, "1.2.3 (commit=abc123, built=2026-06-09T00:00:00Z)", String())
}

func TestFallbackVersion(t *testing.T) {
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

			assert.Equal(t, testCase.expected, fallbackVersion(testCase.version))
		})
	}
}
