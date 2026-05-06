package core

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func normalizeResourcePath(input string) string {
	trimmed := strings.TrimSpace(input)
	home, err := os.UserHomeDir()
	if err != nil {
		return trimmed
	}
	if trimmed == "~" {
		return home
	}
	if strings.HasPrefix(trimmed, "~/") {
		return filepath.Join(home, trimmed[2:])
	}
	if strings.HasPrefix(trimmed, "~") {
		return filepath.Join(home, trimmed[1:])
	}

	return trimmed
}

func resolveResourcePath(path, cwd string) string {
	normalizedPath := normalizeResourcePath(path)
	if filepath.IsAbs(normalizedPath) {
		return filepath.Clean(normalizedPath)
	}

	return filepath.Join(cwd, normalizedPath)
}

func statResource(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

func readResourceFile(path string) (string, error) {
	cleanPath := filepath.Clean(path)
	content, err := fs.ReadFile(os.DirFS(filepath.Dir(cleanPath)), filepath.Base(cleanPath))
	if err != nil {
		return "", err
	}

	return string(content), nil
}

func readResourceDir(path string) ([]os.DirEntry, error) {
	return os.ReadDir(path)
}

func resourcePathExists(path string) bool {
	_, err := statResource(path)
	return err == nil
}

func canonicalizeResourcePath(path string) string {
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return filepath.Clean(path)
	}

	return filepath.Clean(resolvedPath)
}

func isUnderPath(target, root string) bool {
	resolvedTarget := filepath.Clean(target)
	resolvedRoot := filepath.Clean(root)
	if resolvedTarget == resolvedRoot {
		return true
	}
	prefix := resolvedRoot + string(os.PathSeparator)

	return strings.HasPrefix(resolvedTarget, prefix)
}
