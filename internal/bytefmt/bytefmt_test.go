package bytefmt_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/bytefmt"
)

func TestFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		expected  string
		byteCount int64
	}{
		{name: "negative", byteCount: -1, expected: "0B"},
		{name: "zero", byteCount: 0, expected: "0B"},
		{name: "bytes", byteCount: 512, expected: "512B"},
		{name: "kibibytes", byteCount: 1536, expected: "1.5KiB"},
		{name: "mebibytes", byteCount: 2 * 1024 * 1024, expected: "2.0MiB"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.expected, bytefmt.Format(testCase.byteCount))
		})
	}
}
