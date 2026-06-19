package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMatchesSkillIgnoreUsesDoublestarPatterns(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		pattern   string
		name      string
		path      string
		skillName string
		want      bool
	}{
		{
			pattern:   "**/generated/**",
			name:      "SKILL.md",
			skillName: "matches nested doublestar directory",
			path:      "teams/generated/fix/SKILL.md",
			want:      true,
		},
		{
			pattern:   "*.tmp",
			name:      "scratch.tmp",
			skillName: "matches basename glob",
			path:      "skills/scratch.tmp",
			want:      true,
		},
		{
			pattern:   "skills/*.tmp",
			name:      "scratch.tmp",
			skillName: "path glob does not match different directory",
			path:      "archive/scratch.tmp",
			want:      false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.skillName, func(t *testing.T) {
			t.Parallel()

			matched := matchesSkillIgnore(testCase.name, testCase.path, []string{testCase.pattern})

			assert.Equal(t, testCase.want, matched)
		})
	}
}
