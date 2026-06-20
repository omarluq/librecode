package tool

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		expected string
		name     string
		bytes    int
	}{
		{name: "negative", bytes: -1, expected: "0B"},
		{name: "zero", bytes: 0, expected: "0B"},
		{name: "bytes", bytes: 512, expected: "512B"},
		{name: "kilobytes", bytes: 1536, expected: "1.5KiB"},
		{name: "megabytes", bytes: 2 * 1024 * 1024, expected: "2.0MiB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.expected, FormatSize(tt.bytes))
		})
	}
}

func TestTruncateTailKeepsLastLines(t *testing.T) {
	t.Parallel()

	result := TruncateTail("one\ntwo\nthree\nfour", TruncationOptions{MaxLines: 2, MaxBytes: 100})

	assert.True(t, result.Truncated)
	assert.Equal(t, TruncatedByLines, result.TruncatedBy)
	assert.Equal(t, "three\nfour", result.Content)
	assert.Equal(t, 4, result.TotalLines)
	assert.Equal(t, 2, result.OutputLines)
}

func TestTruncateTailReturnsPartialLastLineWhenByteLimitIsSmall(t *testing.T) {
	t.Parallel()

	result := TruncateTail("short\nαβγδε", TruncationOptions{MaxLines: 10, MaxBytes: 5})

	assert.True(t, result.Truncated)
	assert.Equal(t, TruncatedByBytes, result.TruncatedBy)
	assert.True(t, result.LastLinePartial)
	assert.NotContains(t, result.Content, string([]byte{0xef, 0xbf, 0xbd}))
	assert.Equal(t, "δε", result.Content)
}

func TestTruncateTailDoesNotTruncateWhenWithinLimits(t *testing.T) {
	t.Parallel()

	result := TruncateTail("one\ntwo", TruncationOptions{MaxLines: 10, MaxBytes: 100})

	assert.False(t, result.Truncated)
	assert.Equal(t, "one\ntwo", result.Content)
}

func TestTruncateHeadHandlesByteLimitAndFirstLineTooLarge(t *testing.T) {
	t.Parallel()

	bytesResult := TruncateHead("one\ntwo\nthree", TruncationOptions{MaxLines: 10, MaxBytes: 7})
	assert.True(t, bytesResult.Truncated)
	assert.Equal(t, TruncatedByBytes, bytesResult.TruncatedBy)
	assert.Equal(t, "one\ntwo", bytesResult.Content)

	tooLarge := TruncateHead("long-first-line\nsecond", TruncationOptions{MaxLines: 10, MaxBytes: 4})
	assert.True(t, tooLarge.Truncated)
	assert.True(t, tooLarge.FirstLineExceedsLimit)
	assert.Empty(t, tooLarge.Content)
}

func TestTruncateLine(t *testing.T) {
	t.Parallel()

	text, truncated := TruncateLine("hello", 10)
	assert.False(t, truncated)
	assert.Equal(t, "hello", text)

	text, truncated = TruncateLine(strings.Repeat("x", GrepMaxLineLength+1), 0)
	assert.True(t, truncated)
	assert.Len(t, text, GrepMaxLineLength+15)
}
