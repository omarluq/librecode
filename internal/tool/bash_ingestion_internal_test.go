package tool

import (
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSynchronizedBufferCapacity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		captured  string
		truncated bool
	}{
		{
			name: "exact capacity", input: strings.Repeat("x", DefaultMaxBytes),
			captured: strings.Repeat("x", DefaultMaxBytes), truncated: false,
		},
		{
			name: "overflow", input: strings.Repeat("a", DefaultMaxBytes) + strings.Repeat("b", 127),
			captured: strings.Repeat("a", DefaultMaxBytes-127) + strings.Repeat("b", 127), truncated: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			buffer := &synchronizedBuffer{
				buffer: make([]byte, 0, DefaultMaxBytes), total: 0, truncated: false, lock: sync.Mutex{},
			}
			written, err := buffer.Write([]byte(test.input))
			require.NoError(t, err)
			assert.Equal(t, len(test.input), written)

			captured, total, truncated := buffer.snapshot()
			assert.Equal(t, test.captured, string(captured))
			assert.Equal(t, int64(len(test.input)), total)
			assert.Equal(t, test.truncated, truncated)
		})
	}
}

func TestBashReportsIngestionTruncationMetadata(t *testing.T) {
	t.Parallel()

	result, err := NewBashTool(t.TempDir()).Bash(t.Context(), BashInput{
		Command: `printf '%060000d' 0`, Timeout: nil,
	})
	require.NoError(t, err)

	metadata, ok := result.Details[detailTruncation].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, metadata["truncated"])
	assert.Equal(t, "ingestion_bytes", metadata["truncated_by"])
	assert.Equal(t, int64(60000), metadata["total_bytes"])
	assert.Equal(t, DefaultMaxBytes, metadata["retained_bytes"])
	assert.Contains(t, result.Text(), "Output ingestion truncated")
}
