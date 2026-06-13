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
			name: "codex sized context keeps newest third",
			input: RecentTailInput{
				ContextWindow: 272_000,
				CurrentTokens: 0,
			},
			want: 90_666,
		},
		{
			name: "current branch tokens cap model context tail",
			input: RecentTailInput{
				ContextWindow: 272_000,
				CurrentTokens: 75_000,
			},
			want: 25_000,
		},
		{
			name: "current branch token cap rounds up",
			input: RecentTailInput{
				ContextWindow: 272_000,
				CurrentTokens: 2,
			},
			want: 1,
		},
		{
			name: "million token context keeps newest third",
			input: RecentTailInput{
				ContextWindow: 1_000_000,
				CurrentTokens: 0,
			},
			want: 333_333,
		},
		{
			name: "unknown context falls back to fixed tail",
			input: RecentTailInput{
				ContextWindow: 0,
				CurrentTokens: 0,
			},
			want: defaultRecentTailTokens,
		},
		{
			name: "current branch tokens cap unknown context fallback",
			input: RecentTailInput{
				ContextWindow: 0,
				CurrentTokens: 15_000,
			},
			want: 5_000,
		},
		{
			name: "tiny positive context keeps at least one token",
			input: RecentTailInput{
				ContextWindow: 2,
				CurrentTokens: 0,
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
