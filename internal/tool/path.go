package tool

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/text/unicode/norm"
)

const narrowNoBreakSpace = "\u202F"

var unicodeSpaceReplacer = strings.NewReplacer(
	"\u00A0", " ",
	"\u2000", " ",
	"\u2001", " ",
	"\u2002", " ",
	"\u2003", " ",
	"\u2004", " ",
	"\u2005", " ",
	"\u2006", " ",
	"\u2007", " ",
	"\u2008", " ",
	"\u2009", " ",
	"\u200A", " ",
	"\u202F", " ",
	"\u205F", " ",
	"\u3000", " ",
)

// ExpandPath normalizes model-supplied path shorthands.
func ExpandPath(filePath string) string {
	normalizedPath := normalizeAtPrefix(unicodeSpaceReplacer.Replace(filePath))
	homeDirectory, err := os.UserHomeDir()
	if err != nil {
		return normalizedPath
	}
	if normalizedPath == "~" {
		return homeDirectory
	}
	if strings.HasPrefix(normalizedPath, "~/") {
		return filepath.Join(homeDirectory, normalizedPath[2:])
	}

	return normalizedPath
}

// ResolveToCWD resolves relative paths against cwd after path shorthand expansion.
func ResolveToCWD(filePath, cwd string) (string, error) {
	workingDirectory := cwd
	if workingDirectory == "" {
		absoluteCWD, err := filepath.Abs(".")
		if err != nil {
			return "", err
		}
		workingDirectory = absoluteCWD
	}

	expandedPath := ExpandPath(filePath)
	if filepath.IsAbs(expandedPath) {
		return filepath.Clean(expandedPath), nil
	}

	return filepath.Join(workingDirectory, expandedPath), nil
}

// ResolveReadPath resolves a read path and tries common macOS filename variants.
func ResolveReadPath(filePath, cwd string) (string, error) {
	resolvedPath, err := ResolveToCWD(filePath, cwd)
	if err != nil {
		return "", err
	}
	if fileExists(resolvedPath) {
		return resolvedPath, nil
	}

	for _, candidate := range readPathVariants(resolvedPath) {
		if candidate != resolvedPath && fileExists(candidate) {
			return candidate, nil
		}
	}

	return resolvedPath, nil
}

func normalizeAtPrefix(filePath string) string {
	return strings.TrimPrefix(filePath, "@")
}

func fileExists(filePath string) bool {
	//nolint:gosec // Tool paths are intentionally user/model-selected workspace paths.
	_, err := os.Stat(filePath)
	return err == nil || !errors.Is(err, os.ErrNotExist)
}

func readPathVariants(filePath string) []string {
	nfdPath := normNFD(filePath)
	curlyPath := strings.ReplaceAll(filePath, "'", "\u2019")

	return []string{
		tryMacOSScreenshotPath(filePath),
		nfdPath,
		curlyPath,
		strings.ReplaceAll(nfdPath, "'", "\u2019"),
	}
}

func tryMacOSScreenshotPath(filePath string) string {
	replacer := strings.NewReplacer(" AM.", narrowNoBreakSpace+"AM.", " PM.", narrowNoBreakSpace+"PM.")
	return replacer.Replace(filePath)
}

func normNFD(value string) string {
	return norm.NFD.String(value)
}
