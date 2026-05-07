package core_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/core"
)

func TestDefaultResourceLoaderReloadsPiResources(t *testing.T) {
	cwd := newOutsideTempDir(t)
	home := newOutsideTempDir(t)
	agentDir := newOutsideTempDir(t)
	t.Setenv("HOME", home)
	projectDir := filepath.Join(cwd, "project")
	workDir := filepath.Join(projectDir, "pkg", "app")

	writeTestFile(t, filepath.Join(agentDir, "AGENTS.md"), "global context")
	writeTestFile(t, filepath.Join(projectDir, "AGENTS.md"), "project context")
	writeTestFile(t, filepath.Join(workDir, "CLAUDE.md"), "workdir context")
	writeTestFile(
		t,
		filepath.Join(home, core.ConfigDirName, "skills", "user-skill", "SKILL.md"),
		skillMarkdown("user-skill"),
	)
	writeTestFile(t, filepath.Join(workDir, core.ConfigDirName, "prompts", "fix.md"), "Fix $1")
	writeTestFile(t, filepath.Join(workDir, core.ConfigDirName, "SYSTEM.md"), "project system")
	writeTestFile(t, filepath.Join(workDir, core.ConfigDirName, "APPEND_SYSTEM.md"), "append system")

	loader := core.NewDefaultResourceLoader(&core.ResourceLoaderOptions{
		SystemPrompt:                  "",
		AgentDir:                      agentDir,
		CWD:                           workDir,
		AdditionalSkillPaths:          []string{filepath.Join(workDir, "missing-skill")},
		AdditionalPromptTemplatePaths: []string{filepath.Join(workDir, "missing-prompt")},
		AppendSystemPrompt:            nil,
		NoPromptTemplates:             false,
		NoContextFiles:                false,
		NoSkills:                      false,
	})
	require.NoError(t, loader.Reload(context.Background()))

	snapshot := loader.Snapshot()
	assert.Equal(t, "project system", snapshot.SystemPrompt)
	assert.Equal(t, []string{"append system"}, snapshot.AppendSystemPrompt)
	require.Len(t, snapshot.ContextFiles, 3)
	assert.Equal(t, filepath.Join(agentDir, "AGENTS.md"), snapshot.ContextFiles[0].Path)
	assert.Equal(t, filepath.Join(projectDir, "AGENTS.md"), snapshot.ContextFiles[1].Path)
	assert.Equal(t, filepath.Join(workDir, "CLAUDE.md"), snapshot.ContextFiles[2].Path)

	require.Len(t, snapshot.Skills, 1)
	assert.Equal(t, "user-skill", snapshot.Skills[0].Name)
	require.Len(t, snapshot.Prompts, 1)
	assert.Equal(t, "fix", snapshot.Prompts[0].Name)
	assert.Len(t, snapshot.SkillDiagnostics, 1)
	assert.Len(t, snapshot.PromptDiagnostics, 1)
}

func TestDefaultResourceLoaderExtendsResourcesWithSourceInfo(t *testing.T) {
	cwd := newOutsideTempDir(t)
	home := newOutsideTempDir(t)
	agentDir := newOutsideTempDir(t)
	t.Setenv("HOME", home)
	extensionDir := filepath.Join(cwd, "extension")
	skillPath := filepath.Join(extensionDir, "skills", "from-extension", "SKILL.md")
	promptPath := filepath.Join(extensionDir, "prompts", "explain.md")
	writeTestFile(t, skillPath, skillMarkdown("from-extension"))
	writeTestFile(t, promptPath, "Explain $ARGUMENTS")
	sourceInfo := core.NewSourceInfo(extensionDir, core.SourceInfoOptions{
		Scope:   core.SourceScopeTemporary,
		Origin:  core.SourceOriginPackage,
		BaseDir: extensionDir,
		Source:  "extension:test",
	})

	loader := core.NewDefaultResourceLoader(&core.ResourceLoaderOptions{
		SystemPrompt:                  "literal system prompt",
		AgentDir:                      agentDir,
		CWD:                           cwd,
		AdditionalSkillPaths:          nil,
		AdditionalPromptTemplatePaths: nil,
		AppendSystemPrompt:            []string{"literal append"},
		NoPromptTemplates:             true,
		NoContextFiles:                true,
		NoSkills:                      true,
	})
	require.NoError(t, loader.ExtendResources(context.Background(), core.ResourceExtensionPaths{
		SkillPaths:  []core.ResourcePath{{SourceInfo: &sourceInfo, Path: filepath.Dir(skillPath)}},
		PromptPaths: []core.ResourcePath{{SourceInfo: &sourceInfo, Path: promptPath}},
	}))

	snapshot := loader.Snapshot()
	assert.Equal(t, "literal system prompt", snapshot.SystemPrompt)
	assert.Equal(t, []string{"literal append"}, snapshot.AppendSystemPrompt)
	require.Len(t, snapshot.Skills, 1)
	assert.Equal(t, "extension:test", snapshot.Skills[0].SourceInfo.Source)
	require.Len(t, snapshot.Prompts, 1)
	assert.Equal(t, "extension:test", snapshot.Prompts[0].SourceInfo.Source)
	assert.Empty(t, snapshot.ContextFiles)
}

func skillMarkdown(name string) string {
	return frontmatterDelimiter + "\nname: " + name +
		"\ndescription: Test skill " + name + "\n" + frontmatterDelimiter + "\n"
}
