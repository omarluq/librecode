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
	writeCLIFile(
		t,
		filepath.Join(cwd, core.ConfigDirName, "skills", "terminal", "SKILL.md"),
		"---\nname: terminal\ndescription: terminal skill\n---\n",
	)

	snapshot := loadTerminalResources(context.Background(), cwd)

	skillNames := lo.Map(snapshot.Skills, func(skill core.Skill, _ int) string { return skill.Name })
	assert.Contains(t, skillNames, "terminal")
}

func TestLoadTerminalResourcesReturnsEmptySnapshotOnCanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	snapshot := loadTerminalResources(ctx, t.TempDir())

	assert.Empty(t, snapshot.Skills)
}
