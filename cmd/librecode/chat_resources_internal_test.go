package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/core"
)

func TestLoadTerminalResourcesReturnsSnapshot(t *testing.T) {
	cwd := t.TempDir()
	home := t.TempDir()
	t.Setenv("LIBRECODE_HOME", home)
	writeCLIFile(t, filepath.Join(cwd, "AGENTS.md"), "project context")
	writeCLIFile(
		t,
		filepath.Join(cwd, core.ConfigDirName, "skills", "terminal", "SKILL.md"),
		"---\nname: terminal\ndescription: terminal skill\n---\n",
	)

	snapshot := loadTerminalResources(context.Background(), cwd)

	assert.Contains(t, lo.Map(snapshot.ContextFiles, func(contextFile core.ContextFile, _ int) string {
		return contextFile.Path
	}), filepath.Join(cwd, "AGENTS.md"))
	skillNames := lo.Map(snapshot.Skills, func(skill core.Skill, _ int) string { return skill.Name })
	assert.Contains(t, skillNames, "terminal")
}

func TestLoadTerminalResourcesReturnsEmptySnapshotOnCanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	snapshot := loadTerminalResources(ctx, t.TempDir())

	assert.Empty(t, snapshot.ContextFiles)
	assert.Empty(t, snapshot.Skills)
	assert.Empty(t, snapshot.Prompts)
}

func TestTerminalAgentDirFallsBackWhenHomeCannotBeResolved(t *testing.T) {
	t.Setenv("LIBRECODE_HOME", "")
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")

	assert.Equal(t, filepath.Join(".", core.ConfigDirName), terminalAgentDir())
}
