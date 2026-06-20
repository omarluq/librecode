package core

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

func shouldSkipSkillEntry(entry os.DirEntry, path, root string, patterns []gitignore.Pattern) bool {
	name := entry.Name()
	if strings.HasPrefix(name, ".") && !isSkillIgnoreFile(name) {
		return true
	}

	if name == "node_modules" {
		return true
	}

	return matchesSkillIgnore(path, root, isSkillDirEntry(entry, path), patterns)
}

func isSkillDirEntry(entry os.DirEntry, path string) bool {
	if entry.IsDir() {
		return true
	}

	info, err := statResource(path)

	return err == nil && info.IsDir()
}

func readSkillIgnorePatterns(dir, root string) []gitignore.Pattern {
	domain := skillIgnoreDomain(dir, root)
	patterns := []gitignore.Pattern{}

	for _, filename := range []string{".gitignore", ".ignore", ".fdignore"} {
		content, err := readResourceFile(filepath.Join(dir, filename))
		if err != nil {
			continue
		}

		for line := range strings.SplitSeq(content, "\n") {
			pattern := strings.TrimSpace(line)
			if pattern == "" || strings.HasPrefix(pattern, "#") {
				continue
			}

			patterns = append(patterns, gitignore.ParsePattern(pattern, domain))
		}
	}

	return patterns
}

func skillIgnoreDomain(dir, root string) []string {
	relativePath, err := filepath.Rel(root, dir)
	if err != nil || relativePath == "." || relativePath == "" {
		return nil
	}

	return strings.Split(filepath.ToSlash(relativePath), "/")
}

func matchesSkillIgnore(path, root string, isDir bool, patterns []gitignore.Pattern) bool {
	if len(patterns) == 0 {
		return false
	}

	pathParts := skillIgnorePathParts(path, root)
	if len(pathParts) == 0 {
		return false
	}

	return gitignore.NewMatcher(patterns).Match(pathParts, isDir)
}

func skillIgnorePathParts(path, root string) []string {
	relativePath, err := filepath.Rel(root, path)
	if err != nil || skillIgnorePathEscapesRoot(relativePath) {
		return nil
	}

	return strings.Split(filepath.ToSlash(relativePath), "/")
}

func skillIgnorePathEscapesRoot(relativePath string) bool {
	return relativePath == "." ||
		relativePath == "" ||
		strings.HasPrefix(relativePath, "..") ||
		filepath.IsAbs(relativePath)
}

func isSkillIgnoreFile(name string) bool {
	return name == ".gitignore" || name == ".ignore" || name == ".fdignore"
}
