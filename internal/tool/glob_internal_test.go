package tool

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGlobMatcherUsesDoublestarPatterns(t *testing.T) {
	t.Parallel()

	const nestedGoPath = "cmd/librecode/main.go"

	testCases := []struct {
		pattern string
		path    string
		name    string
		want    bool
	}{
		{
			pattern: "**/*.go",
			path:    "main.go",
			name:    "doublestar matches root file",
			want:    true,
		},
		{
			pattern: "**/*.go",
			path:    nestedGoPath,
			name:    "doublestar matches nested file",
			want:    true,
		},
		{
			pattern: "*.go",
			path:    nestedGoPath,
			name:    "basename glob matches nested file basename",
			want:    true,
		},
		{
			pattern: "cmd/*.go",
			path:    nestedGoPath,
			name:    "path glob only matches exact path shape",
			want:    false,
		},
		{
			pattern: "cmd/**/*.go",
			path:    nestedGoPath,
			name:    "path doublestar matches nested path",
			want:    true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			matcher, err := newGlobMatcher(testCase.pattern)
			require.NoError(t, err)

			assert.Equal(t, testCase.want, matcher(testCase.path))
		})
	}
}

func TestGlobMatcherRejectsInvalidPattern(t *testing.T) {
	t.Parallel()

	_, err := newGlobMatcher("[")

	require.Error(t, err)
}
