package core_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/core"
)

func TestLoadSkillsDiscoversConfiguredRootsAndFormatsPrompt(t *testing.T) {
	cwd := t.TempDir()
	home := t.TempDir()
	agentDir := t.TempDir()
	t.Setenv("HOME", home)

	writeTestFile(t, filepath.Join(cwd, core.ConfigDirName, "skills", "hidden", "SKILL.md"), strings.Join([]string{
		frontmatterDelimiter,
		"name: hidden",
		"description: Hidden skill",
		"disable-model-invocation: true",
		frontmatterDelimiter,
		"Only explicit calls.",
	}, "\n"))
	writeTestFile(t, filepath.Join(cwd, core.AgentsDirName, "skills", "fix-bug", "SKILL.md"), skillMarkdown("fix-bug"))
	writeTestFile(
		t,
		filepath.Join(home, core.ConfigDirName, "skills", "user-libre", "SKILL.md"),
		skillMarkdown("user-libre"),
	)
	writeTestFile(
		t,
		filepath.Join(home, core.AgentsDirName, "skills", "user-agent", "SKILL.md"),
		skillMarkdown("user-agent"),
	)
	writeTestFile(t, filepath.Join(agentDir, "skills", "legacy-agent", "SKILL.md"), skillMarkdown("legacy-agent"))

	result := core.LoadSkills(cwd, nil, true)
	require.Empty(t, result.Diagnostics)
	require.Len(t, result.Skills, 4)

	assert.Equal(
		t,
		[]string{"hidden", "fix-bug", "user-libre", "user-agent"},
		skillNames(result.Skills),
	)
	assert.Equal(t, core.SourceScopeProject, result.Skills[0].SourceInfo.Scope)
	assert.Equal(t, core.SourceScopeProject, result.Skills[1].SourceInfo.Scope)
	assert.Equal(t, core.SourceScopeUser, result.Skills[2].SourceInfo.Scope)
	assert.Equal(t, core.SourceScopeUser, result.Skills[3].SourceInfo.Scope)

	prompt := core.FormatSkillsForPrompt(result.Skills)
	assert.Contains(t, prompt, "<name>fix-bug</name>")
	assert.Contains(t, prompt, "<name>user-libre</name>")
	assert.Contains(t, prompt, "<name>user-agent</name>")
	assert.NotContains(t, prompt, "<name>legacy-agent</name>")
	assert.NotContains(t, prompt, "<name>hidden</name>")
}

func TestLoadSkillsDedupesByPriority(t *testing.T) {
	cwd := t.TempDir()
	home := t.TempDir()
	agentDir := t.TempDir()
	t.Setenv("HOME", home)

	winnerPath := filepath.Join(cwd, core.ConfigDirName, "skills", "same", "SKILL.md")
	writeTestFile(t, winnerPath, skillMarkdown("same"))
	writeTestFile(t, filepath.Join(cwd, core.AgentsDirName, "skills", "same", "SKILL.md"), skillMarkdown("same"))
	writeTestFile(t, filepath.Join(home, core.ConfigDirName, "skills", "same", "SKILL.md"), skillMarkdown("same"))
	writeTestFile(t, filepath.Join(home, core.AgentsDirName, "skills", "same", "SKILL.md"), skillMarkdown("same"))
	writeTestFile(t, filepath.Join(agentDir, "skills", "same", "SKILL.md"), skillMarkdown("same"))

	result := core.LoadSkills(cwd, nil, true)
	require.Len(t, result.Skills, 1)
	assert.Equal(t, winnerPath, result.Skills[0].FilePath)
	require.Len(t, result.Diagnostics, 3)
	for _, diagnostic := range result.Diagnostics {
		require.NotNil(t, diagnostic.Collision)
		assert.Equal(t, "same", diagnostic.Collision.Name)
		assert.Equal(t, winnerPath, diagnostic.Collision.WinnerPath)
	}
}

func TestLoadSkillsParsesSpecFrontmatter(t *testing.T) {
	cwd := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeTestFile(t, filepath.Join(cwd, core.ConfigDirName, "skills", "spec-skill", "SKILL.md"), strings.Join([]string{
		frontmatterDelimiter,
		"name: spec-skill",
		"description: Use for spec parsing",
		"license: MIT",
		"compatibility: Works with librecode",
		"allowed-tools: Bash(git:*) Read",
		"user-invocable: true",
		"metadata:",
		"  author: omar",
		frontmatterDelimiter,
		"Body.",
	}, "\n"))

	result := core.LoadSkills(cwd, nil, true)

	require.Empty(t, result.Diagnostics)
	require.Len(t, result.Skills, 1)
	skill := result.Skills[0]
	assert.Equal(t, "MIT", skill.License)
	assert.Equal(t, "Works with librecode", skill.Compatibility)
	assert.Equal(t, []string{"Bash(git:*)", "Read"}, skill.AllowedTools)
	assert.True(t, skill.UserInvocable)
	assert.Equal(t, "omar", skill.Metadata["author"])
}

func TestLoadSkillsHonorsIgnoreFiles(t *testing.T) {
	cwd := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	skillsDir := filepath.Join(cwd, core.ConfigDirName, "skills")
	writeTestFile(t, filepath.Join(skillsDir, ".ignore"), "ignored-skill\n")
	writeTestFile(t, filepath.Join(skillsDir, "ignored-skill", "SKILL.md"), skillMarkdown("ignored-skill"))
	writeTestFile(t, filepath.Join(skillsDir, "kept-skill", "SKILL.md"), skillMarkdown("kept-skill"))

	result := core.LoadSkills(cwd, nil, true)

	require.Empty(t, result.Diagnostics)
	assert.Equal(t, []string{"kept-skill"}, skillNames(result.Skills))
}

func TestAutoActivateSkillsSelectsMatchingSkill(t *testing.T) {
	cwd := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTestFile(t, filepath.Join(cwd, core.ConfigDirName, "skills", "bug-fix", "SKILL.md"), strings.Join([]string{
		frontmatterDelimiter,
		"name: bug-fix",
		"description: Use when fixing bugs safely",
		frontmatterDelimiter,
		"Run tests before editing.",
	}, "\n"))

	result := core.LoadSkills(cwd, nil, true)
	activated, diagnostics := core.AutoActivateSkills("please use bug-fix safely", result.Skills)

	require.Empty(t, diagnostics)
	require.Len(t, activated, 1)
	assert.Equal(t, "bug-fix", activated[0].Skill.Name)
	assert.Contains(t, activated[0].Content, "Run tests")
}

func TestAutoActivateSkillsRequiresStrongIntent(t *testing.T) {
	cwd := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTestFile(t, filepath.Join(cwd, core.ConfigDirName, "skills", "hud", "SKILL.md"), strings.Join([]string{
		frontmatterDelimiter,
		"name: hud",
		"description: Show or configure the runtime HUD status display",
		frontmatterDelimiter,
		"HUD instructions.",
	}, "\n"))
	writeTestFile(t, filepath.Join(cwd, core.ConfigDirName, "skills", "go-tests", "SKILL.md"), strings.Join([]string{
		frontmatterDelimiter,
		"name: go-tests",
		"description: Use when writing Go tests or debugging flaky test failures",
		frontmatterDelimiter,
		"Testing instructions.",
	}, "\n"))

	result := core.LoadSkills(cwd, nil, true)

	activated, diagnostics := core.AutoActivateSkills("hello", result.Skills)
	require.Empty(t, diagnostics)
	assert.Empty(t, activated)

	activated, diagnostics = core.AutoActivateSkills("hud", result.Skills)
	require.Empty(t, diagnostics)
	require.Len(t, activated, 1)
	assert.Equal(t, "hud", activated[0].Skill.Name)

	activated, diagnostics = core.AutoActivateSkills("please write Go tests for this package", result.Skills)
	require.Empty(t, diagnostics)
	require.Len(t, activated, 1)
	assert.Equal(t, "go-tests", activated[0].Skill.Name)
}

func TestLoadSkillsReportsValidationWarningsAndNameCollisions(t *testing.T) {
	cwd := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeTestFile(t, filepath.Join(home, core.AgentsDirName, "skills", "same", "SKILL.md"), strings.Join([]string{
		frontmatterDelimiter,
		"name: same",
		"description: First skill",
		frontmatterDelimiter,
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(cwd, core.ConfigDirName, "skills", "same", "SKILL.md"), strings.Join([]string{
		frontmatterDelimiter,
		"name: same",
		"description: Second skill",
		frontmatterDelimiter,
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(cwd, core.ConfigDirName, "skills", "Bad_Name", "SKILL.md"), strings.Join([]string{
		frontmatterDelimiter,
		"name: Bad_Name",
		"description: Invalid name",
		frontmatterDelimiter,
		"",
	}, "\n"))

	result := core.LoadSkills(cwd, nil, true)
	require.Len(t, result.Skills, 2)
	require.Len(t, result.Diagnostics, 2)

	assert.True(t, hasCollision(result.Diagnostics, "same"))
	assert.True(t, hasDiagnosticMessage(result.Diagnostics, "invalid characters"))
}

func skillNames(skills []core.Skill) []string {
	names := make([]string, 0, len(skills))
	for index := range skills {
		names = append(names, skills[index].Name)
	}

	return names
}

func hasCollision(diagnostics []core.ResourceDiagnostic, name string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Collision != nil && diagnostic.Collision.Name == name {
			return true
		}
	}

	return false
}

func hasDiagnosticMessage(diagnostics []core.ResourceDiagnostic, message string) bool {
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic.Message, message) {
			return true
		}
	}

	return false
}
