package vinfo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStringUsesInjectedBuildMetadata(t *testing.T) {
	t.Parallel()

	oldVersion := version

	t.Cleanup(func() {
		version = oldVersion
	})

	version = "1.2.3"

	assert.Equal(t, "1.2.3 (commit=none, built=unknown)", String())
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
