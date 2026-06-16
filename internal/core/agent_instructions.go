package core

import (
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	agentInstructionsMaxBytes       = 32 * 1024
	agentInstructionSectionCapacity = 4
	agentsOverrideFileName          = "AGENTS.override.md"
	agentsFileName                  = "AGENTS.md"
	agentFileName                   = "AGENT.md"
)

// LoadAgentInstructions loads global and project AGENTS.md-style instructions
// for cwd. It follows Codex-compatible precedence: one file from the global
// LibreCode home, then one file per project directory from root to cwd.
func LoadAgentInstructions(cwd string) string {
	trimmedCWD := strings.TrimSpace(cwd)
	if trimmedCWD == "" {
		return ""
	}

	sections := make([]string, 0, agentInstructionSectionCapacity)
	remaining := agentInstructionsMaxBytes

	if home, err := LibrecodeHome(); err == nil {
		sections, remaining = appendAgentInstructionSection(sections, home, remaining)
	}

	for _, dir := range agentInstructionDirs(trimmedCWD) {
		sections, remaining = appendAgentInstructionSection(sections, dir, remaining)
		if remaining == 0 {
			break
		}
	}

	return strings.Join(sections, "\n\n")
}

func appendAgentInstructionSection(
	sections []string,
	dir string,
	remaining int,
) (updatedSections []string, updatedRemaining int) {
	if remaining <= 0 {
		return sections, 0
	}

	content := readFirstAgentInstruction(dir)
	if content == "" {
		return sections, remaining
	}

	if len(content) > remaining {
		content = truncateAgentInstruction(content, remaining)
		remaining = 0
	} else {
		remaining -= len(content)
	}

	if content == "" {
		return sections, remaining
	}

	return append(sections, content), remaining
}

func truncateAgentInstruction(content string, limit int) string {
	if limit <= 0 {
		return ""
	}

	end := 0
	for end < len(content) {
		_, size := utf8.DecodeRuneInString(content[end:])
		if end+size > limit {
			break
		}

		end += size
	}

	return content[:end]
}

func readFirstAgentInstruction(dir string) string {
	for _, name := range []string{agentsOverrideFileName, agentsFileName, agentFileName} {
		content, ok := readAgentInstructionFile(filepath.Join(dir, name))
		if ok {
			return content
		}
	}

	return ""
}

func readAgentInstructionFile(path string) (string, bool) {
	content, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return "", false
	}

	trimmed := strings.TrimSpace(string(content))
	if trimmed == "" {
		return "", false
	}

	return trimmed, true
}

func agentInstructionDirs(cwd string) []string {
	cleanCWD := filepath.Clean(cwd)

	root := projectRoot(cleanCWD)
	if root == "" {
		return []string{cleanCWD}
	}

	relativePath, err := filepath.Rel(root, cleanCWD)
	if err != nil || relativePath == "." {
		return []string{root}
	}

	dirs := []string{root}
	current := root
	elements := strings.SplitSeq(relativePath, string(os.PathSeparator))

	for element := range elements {
		if element == "." || element == "" {
			continue
		}

		current = filepath.Join(current, element)
		dirs = append(dirs, current)
	}

	return dirs
}

func projectRoot(cwd string) string {
	current := filepath.Clean(cwd)
	for {
		if resourcePathExists(filepath.Join(current, ".git")) {
			return current
		}

		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}

		current = parent
	}
}
