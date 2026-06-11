package contextwindow

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRecentTailTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input RecentTailInput
		want  int
	}{
		{
			name: "explicit override wins",
			input: RecentTailInput{
				ExplicitKeepRecentTokens: 12_345,
				ContextWindow:            272_000,
			},
			want: 12_345,
		},
		{
			name: "codex sized context keeps newest third",
			input: RecentTailInput{
				ExplicitKeepRecentTokens: 0,
				ContextWindow:            272_000,
			},
			want: 90_666,
		},
		{
			name: "million token context keeps newest third",
			input: RecentTailInput{
				ExplicitKeepRecentTokens: 0,
				ContextWindow:            1_000_000,
			},
			want: 333_333,
		},
		{
			name: "unknown context falls back to fixed tail",
			input: RecentTailInput{
				ExplicitKeepRecentTokens: 0,
				ContextWindow:            0,
			},
			want: defaultKeepRecentTokens,
		},
		{
			name: "tiny positive context keeps at least one token",
			input: RecentTailInput{
				ExplicitKeepRecentTokens: 0,
				ContextWindow:            2,
			},
			want: 1,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, RecentTailTarget(testCase.input))
		})
	}
}
