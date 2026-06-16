package core_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/core"
)

func TestLoadAgentInstructionsLayersGlobalAndProjectFiles(t *testing.T) {
	home := newOutsideTempDir(t)
	cwd := filepath.Join(newOutsideTempDir(t), "repo", "service")
	repo := filepath.Dir(cwd)
	writeTestFile(t, filepath.Join(cwd, ".keep"), "")
	t.Setenv("LIBRECODE_HOME", filepath.Join(home, core.ConfigDirName))

	writeTestFile(t, filepath.Join(home, core.ConfigDirName, "AGENTS.md"), "global")
	writeTestFile(t, filepath.Join(repo, ".git"), "gitdir: .git")
	writeTestFile(t, filepath.Join(repo, "AGENTS.md"), "repo")
	writeTestFile(t, filepath.Join(cwd, "AGENT.md"), "service")

	instructions := core.LoadAgentInstructions(cwd)

	assert.Equal(t, "global\n\nrepo\n\nservice", instructions)
}

func TestLoadAgentInstructionsPrefersOverridePerDirectory(t *testing.T) {
	cwd := filepath.Join(newOutsideTempDir(t), "repo")
	writeTestFile(t, filepath.Join(cwd, ".keep"), "")
	t.Setenv("LIBRECODE_HOME", filepath.Join(newOutsideTempDir(t), core.ConfigDirName))

	writeTestFile(t, filepath.Join(cwd, ".git"), "gitdir: .git")
	writeTestFile(t, filepath.Join(cwd, "AGENTS.md"), "base")
	writeTestFile(t, filepath.Join(cwd, "AGENTS.override.md"), "override")

	assert.Equal(t, "override", core.LoadAgentInstructions(cwd))
}

func TestLoadAgentInstructionsUsesOnlyCWDWhenProjectRootMissing(t *testing.T) {
	cwd := filepath.Join(newOutsideTempDir(t), "plain")
	writeTestFile(t, filepath.Join(cwd, ".keep"), "")
	t.Setenv("LIBRECODE_HOME", filepath.Join(newOutsideTempDir(t), core.ConfigDirName))
	writeTestFile(t, filepath.Join(cwd, "AGENTS.md"), "local")

	assert.Equal(t, "local", core.LoadAgentInstructions(cwd))
}

func TestLoadAgentInstructionsSkipsEmptyFiles(t *testing.T) {
	cwd := filepath.Join(newOutsideTempDir(t), "repo")
	writeTestFile(t, filepath.Join(cwd, ".keep"), "")
	t.Setenv("LIBRECODE_HOME", filepath.Join(newOutsideTempDir(t), core.ConfigDirName))

	writeTestFile(t, filepath.Join(cwd, ".git"), "gitdir: .git")
	writeTestFile(t, filepath.Join(cwd, "AGENTS.override.md"), "  \n")
	writeTestFile(t, filepath.Join(cwd, "AGENTS.md"), "base")

	assert.Equal(t, "base", core.LoadAgentInstructions(cwd))
}

func TestLoadAgentInstructionsCapsCombinedSize(t *testing.T) {
	cwd := filepath.Join(newOutsideTempDir(t), "repo")
	writeTestFile(t, filepath.Join(cwd, ".keep"), "")
	t.Setenv("LIBRECODE_HOME", filepath.Join(newOutsideTempDir(t), core.ConfigDirName))

	writeTestFile(t, filepath.Join(cwd, ".git"), "gitdir: .git")
	writeTestFile(t, filepath.Join(cwd, "AGENTS.md"), strings.Repeat("a", 40_000))

	assert.Len(t, core.LoadAgentInstructions(cwd), 32*1024)
}

func TestLoadAgentInstructionsTruncatesAtUTF8Boundary(t *testing.T) {
	cwd := filepath.Join(newOutsideTempDir(t), "repo")
	writeTestFile(t, filepath.Join(cwd, ".keep"), "")
	t.Setenv("LIBRECODE_HOME", filepath.Join(newOutsideTempDir(t), core.ConfigDirName))

	writeTestFile(t, filepath.Join(cwd, ".git"), "gitdir: .git")
	writeTestFile(t, filepath.Join(cwd, "AGENTS.md"), strings.Repeat("a", 32*1024-1)+"🙂")

	assert.Equal(t, strings.Repeat("a", 32*1024-1), core.LoadAgentInstructions(cwd))
}
