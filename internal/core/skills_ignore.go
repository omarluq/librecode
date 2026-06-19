package core

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

func shouldSkipSkillEntry(entry os.DirEntry, path string, ignorePatterns []string) bool {
	name := entry.Name()
	if strings.HasPrefix(name, ".") && !isSkillIgnoreFile(name) {
		return true
	}

	if name == "node_modules" {
		return true
	}

	return matchesSkillIgnore(name, path, ignorePatterns)
}

func isSkillDirEntry(entry os.DirEntry, path string) bool {
	if entry.IsDir() {
		return true
	}

	info, err := statResource(path)

	return err == nil && info.IsDir()
}

func readSkillIgnorePatterns(dir string) []string {
	patterns := []string{}

	for _, filename := range []string{".gitignore", ".ignore", ".fdignore"} {
		content, err := readResourceFile(filepath.Join(dir, filename))
		if err != nil {
			continue
		}

		for line := range strings.SplitSeq(content, "\n") {
			pattern := strings.TrimSpace(line)
			if pattern == "" || strings.HasPrefix(pattern, "#") || strings.HasPrefix(pattern, "!") {
				continue
			}

			patterns = append(patterns, pattern)
		}
	}

	return patterns
}

func matchesSkillIgnore(name, path string, patterns []string) bool {
	slashPath := filepath.ToSlash(path)

	for _, pattern := range patterns {
		trimmed := strings.Trim(pattern, "/")
		if trimmed == "" {
			continue
		}

		if trimmed == name || strings.HasSuffix(slashPath, "/"+trimmed) {
			return true
		}

		if !doublestar.ValidatePattern(trimmed) {
			continue
		}

		if doublestar.MatchUnvalidated(trimmed, name) {
			return true
		}

		if doublestar.MatchUnvalidated(trimmed, slashPath) {
			return true
		}
	}

	return false
}

func isSkillIgnoreFile(name string) bool {
	return name == ".gitignore" || name == ".ignore" || name == ".fdignore"
}
