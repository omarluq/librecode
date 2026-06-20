package core

import (
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
	"github.com/stretchr/testify/assert"
)

func TestMatchesSkillIgnoreUsesGitignorePatterns(t *testing.T) {
	t.Parallel()

	const scratchTmp = "scratch.tmp"

	testCases := []struct {
		pattern string
		name    string
		path    string
		want    bool
	}{
		{
			pattern: "**/generated/**",
			name:    "matches nested generated directory",
			path:    filepath.Join("skills", "teams", "generated", "fix", "SKILL.md"),
			want:    true,
		},
		{
			pattern: "*.tmp",
			name:    "matches basename glob",
			path:    filepath.Join("skills", scratchTmp),
			want:    true,
		},
		{
			pattern: "skills/*.tmp",
			name:    "path glob does not match different directory",
			path:    filepath.Join("archive", scratchTmp),
			want:    false,
		},
		{
			pattern: "ignored/",
			name:    "matches directories",
			path:    filepath.Join("skills", "ignored"),
			want:    true,
		},
		{
			pattern: "!kept/",
			name:    "supports negation",
			path:    filepath.Join("skills", "kept"),
			want:    false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			path := filepath.Join(root, testCase.path)
			patterns := []gitignore.Pattern{gitignore.ParsePattern(testCase.pattern, nil)}
			isDir := testCase.pattern == "ignored/" || testCase.pattern == "!kept/"

			matched := matchesSkillIgnore(path, root, isDir, patterns)

			assert.Equal(t, testCase.want, matched)
		})
	}
}
