package core_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/core"
)

func TestDefaultResourceLoaderReloadsSkills(t *testing.T) {
	cwd := newOutsideTempDir(t)
	home := newOutsideTempDir(t)
	t.Setenv("HOME", home)

	writeTestFile(
		t,
		filepath.Join(home, core.ConfigDirName, "skills", "user-skill", "SKILL.md"),
		skillMarkdown("user-skill"),
	)

	loader := core.NewDefaultResourceLoader(cwd)
	require.NoError(t, loader.Reload(context.Background()))

	snapshot := loader.Snapshot()
	require.Len(t, snapshot.Skills, 1)
	assert.Equal(t, "user-skill", snapshot.Skills[0].Name)
	assert.Empty(t, snapshot.SkillDiagnostics)
}

func TestDefaultResourceLoaderLoadsAgentInstructions(t *testing.T) {
	t.Parallel()

	cwd := newOutsideTempDir(t)
	writeTestFile(t, filepath.Join(cwd, "AGENTS.md"), "project instructions")

	loader := core.NewDefaultResourceLoader(cwd)
	require.NoError(t, loader.Reload(context.Background()))

	assert.Equal(t, "project instructions", loader.Snapshot().AgentInstructions)
}

func skillMarkdown(name string) string {
	return frontmatterDelimiter + "\nname: " + name +
		"\ndescription: Test skill " + name + "\n" + frontmatterDelimiter + "\n"
}
