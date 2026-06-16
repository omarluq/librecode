package tool

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLimitReachedNotice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		suffix   string
		expected string
	}{
		{
			name:     "without suffix",
			suffix:   "",
			expected: "5 entries limit reached. Use limit=10 for more",
		},
		{
			name:     "with suffix",
			suffix:   "or refine pattern",
			expected: "5 entries limit reached. Use limit=10 for more, or refine pattern",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.expected, limitReachedNotice("entries", 5, testCase.suffix))
		})
	}
}
