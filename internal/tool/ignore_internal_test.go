package tool

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadIgnorePatternsAppliesDefaultsWithoutUserConfig(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeIgnoreTestFile(t, filepath.Join(workspace, "readme.md"), "content")

	patterns := readIgnorePatterns(workspace, nil)

	sources := patternSources(patterns)
	assert.Equal(t, defaultIgnorePatternSources(), sources)
	assert.False(t, slices.IsSorted(sources) && len(sources) != len(defaultIgnorePatternSources()))
}

func TestReadIgnorePatternsReturnsSharedDefaultsWithoutMutatingThem(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeIgnoreTestFile(t, filepath.Join(workspace, ".gitignore"), "*.log\n")

	before := slices.Clone(defaultReadIgnorePatterns)
	patterns := readIgnorePatterns(workspace, nil)
	require.NotEmpty(t, patterns)
	patterns[0].source = "mutated"

	assert.Equal(t, before, defaultReadIgnorePatterns)
}

func TestIgnoredReadPathReasonsMatchDefaultAndCustomPatterns(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeIgnoreTestFile(t, filepath.Join(workspace, ".gitignore"), "*.log\n!important.log\n")
	writeIgnoreTestFile(t, filepath.Join(workspace, defaultIgnoreEnv), "SECRET=value")

	tests := []struct {
		name       string
		relative   string
		wantReason string
		wantIgnore bool
	}{
		{name: "default pattern reports its source", relative: defaultIgnoreEnv, wantIgnore: true,
			wantReason: defaultIgnoreEnv},
		{
			name:       "repository pattern reports gitignore source",
			relative:   "debug.log",
			wantIgnore: true,
			wantReason: ".gitignore",
		},
		{name: "negated repository pattern allows file", relative: "important.log", wantIgnore: false, wantReason: ""},
		{name: "non-ignored file is allowed", relative: "readme.md", wantIgnore: false, wantReason: ""},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			absolutePath := filepath.Join(workspace, filepath.FromSlash(testCase.relative))
			writeIgnoreTestFile(t, absolutePath, "content")

			ignored, reason := ignoredReadPath(absolutePath, workspace, nil)

			assert.Equal(t, testCase.wantIgnore, ignored)
			assert.Equal(t, testCase.wantReason, reason)
		})
	}
}

func TestDefaultReadIgnorePatternsMatchingMatchesFreshlyParsedPatterns(t *testing.T) {
	t.Parallel()

	paths := [][]string{
		{gitDirName, "config"},
		{"node_modules", "pkg", "index.js"},
		{defaultIgnoreEnv},
		{"nested", defaultIgnoreEnv},
		{".gocache", "cache"},
		{".gomodcache", "pkg"},
		{".tmp", "scratch"},
		{"bin", "librecode"},
		{"skills", "skill.md"},
		{".agents", "skills", "skill.md"},
		{"cmd", "main.go"},
	}

	fresh := make([]ignorePattern, 0, len(defaultIgnorePatternSources()))
	for _, source := range defaultIgnorePatternSources() {
		fresh = append(fresh, ignorePattern{pattern: gitignore.ParsePattern(source, nil), source: source})
	}

	for _, pathParts := range paths {
		isDir := filepath.Ext(pathParts[len(pathParts)-1]) == ""
		assert.Equal(
			t,
			matchingIgnoreReason(fresh, pathParts, isDir),
			matchingIgnoreReason(defaultReadIgnorePatterns, pathParts, isDir),
			"path %v", pathParts,
		)
	}
}

func defaultIgnorePatternSources() []string {
	return []string{
		".git/",
		"node_modules/",
		defaultIgnoreEnv,
		".gocache/",
		".gomodcache/",
		".tmp/",
		"bin/",
		"/skills/",
	}
}

func patternSources(patterns []ignorePattern) []string {
	sources := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		sources = append(sources, pattern.source)
	}

	return sources
}

func writeIgnoreTestFile(t *testing.T, path, content string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}
