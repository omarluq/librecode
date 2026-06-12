package anthropicmodel_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/anthropicmodel"
)

func TestCapabilities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		modelID          string
		wantAdaptive     bool
		wantRequired     bool
		wantSupportsHigh bool
	}{
		{
			name:             "fable",
			modelID:          anthropicmodel.Fable5,
			wantAdaptive:     true,
			wantRequired:     true,
			wantSupportsHigh: true,
		},
		{
			name:             "mythos",
			modelID:          anthropicmodel.Mythos5,
			wantAdaptive:     true,
			wantRequired:     true,
			wantSupportsHigh: true,
		},
		{
			name:             "mythos preview",
			modelID:          "claude-mythos-preview",
			wantAdaptive:     true,
			wantRequired:     true,
			wantSupportsHigh: true,
		},
		{
			name:             "opus 4.8",
			modelID:          "claude-opus-4-8",
			wantAdaptive:     true,
			wantRequired:     false,
			wantSupportsHigh: true,
		},
		{
			name:             "sonnet 4.6",
			modelID:          "claude-sonnet-4-6",
			wantAdaptive:     true,
			wantRequired:     false,
			wantSupportsHigh: false,
		},
		{
			name:             "sonnet 4.5",
			modelID:          "claude-sonnet-4-5",
			wantAdaptive:     false,
			wantRequired:     false,
			wantSupportsHigh: false,
		},
		{
			name:             "mythos false positive",
			modelID:          "claude-mythos-50",
			wantAdaptive:     false,
			wantRequired:     false,
			wantSupportsHigh: false,
		},
		{
			name:             "opus false positive",
			modelID:          "claude-opus-4-70",
			wantAdaptive:     false,
			wantRequired:     false,
			wantSupportsHigh: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.wantAdaptive, anthropicmodel.SupportsAdaptiveThinking(test.modelID))
			assert.Equal(t, test.wantRequired, anthropicmodel.RequiresAdaptiveThinking(test.modelID))
			assert.Equal(t, test.wantSupportsHigh, anthropicmodel.SupportsXHigh(test.modelID))
		})
	}
}
