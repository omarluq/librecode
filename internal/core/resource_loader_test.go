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

	loader := core.NewDefaultResourceLoader(&core.ResourceLoaderOptions{
		CWD:                  cwd,
		AdditionalSkillPaths: []string{filepath.Join(cwd, "missing-skill")},
		NoSkills:             false,
	})
	require.NoError(t, loader.Reload(context.Background()))

	snapshot := loader.Snapshot()
	assert.Equal(t, snapshot.Skills, loader.Skills().Skills)
	require.Len(t, snapshot.Skills, 1)
	assert.Equal(t, "user-skill", snapshot.Skills[0].Name)
	assert.Len(t, snapshot.SkillDiagnostics, 1)
}

func TestDefaultResourceLoaderLoadsAgentInstructions(t *testing.T) {
	t.Parallel()

	cwd := newOutsideTempDir(t)
	writeTestFile(t, filepath.Join(cwd, "AGENTS.md"), "project instructions")

	loader := core.NewDefaultResourceLoader(&core.ResourceLoaderOptions{
		CWD:                  cwd,
		AdditionalSkillPaths: nil,
		NoSkills:             true,
	})
	require.NoError(t, loader.Reload(context.Background()))

	assert.Equal(t, "project instructions", loader.Snapshot().AgentInstructions)
}

func TestDefaultResourceLoaderExtendsSkillResourcesWithSourceInfo(t *testing.T) {
	t.Parallel()

	cwd := newOutsideTempDir(t)

	extensionDir := filepath.Join(cwd, "extension")
	skillPath := filepath.Join(extensionDir, "skills", "from-extension", "SKILL.md")
	writeTestFile(t, skillPath, skillMarkdown("from-extension"))

	sourceInfo := core.NewSourceInfo(extensionDir, core.SourceInfoOptions{
		Scope:   core.SourceScopeTemporary,
		Origin:  core.SourceOriginPackage,
		BaseDir: extensionDir,
		Source:  "extension:test",
	})

	loader := core.NewDefaultResourceLoader(&core.ResourceLoaderOptions{
		CWD:                  cwd,
		AdditionalSkillPaths: nil,
		NoSkills:             true,
	})
	require.NoError(t, loader.ExtendResources(context.Background(), core.ResourceExtensionPaths{
		SkillPaths: []core.ResourcePath{{SourceInfo: &sourceInfo, Path: filepath.Dir(skillPath)}},
	}))

	snapshot := loader.Snapshot()
	require.Len(t, snapshot.Skills, 1)
	assert.Equal(t, "extension:test", snapshot.Skills[0].SourceInfo.Source)
}

func skillMarkdown(name string) string {
	return frontmatterDelimiter + "\nname: " + name +
		"\ndescription: Test skill " + name + "\n" + frontmatterDelimiter + "\n"
}
