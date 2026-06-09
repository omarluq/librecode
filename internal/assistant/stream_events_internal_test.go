package assistant

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/provider"
)

func TestProviderStreamEventKindMapsKnownKinds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind provider.StreamEventKind
		want StreamEventKind
	}{
		{name: "text delta", kind: provider.StreamEventTextDelta, want: StreamEventTextDelta},
		{name: "thinking delta", kind: provider.StreamEventThinkingDelta, want: StreamEventThinkingDelta},
		{name: "tool start", kind: provider.StreamEventToolStart, want: StreamEventToolStart},
		{name: "tool result", kind: provider.StreamEventToolResult, want: StreamEventToolResult},
		{name: "unknown", kind: provider.StreamEventKind("provider_new_event"), want: StreamEventUnknown},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.want, providerStreamEventKind(test.kind))
		})
	}
}
