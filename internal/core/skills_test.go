package core_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/core"
)

func TestLoadSkillsDiscoversSkillRootsAndFormatsPrompt(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	agentDir := t.TempDir()
	writeTestFile(t, filepath.Join(agentDir, "skills", "fix-bug", "SKILL.md"), strings.Join([]string{
		frontmatterDelimiter,
		"name: fix-bug",
		"description: Fix bugs safely",
		frontmatterDelimiter,
		"Use tests first.",
	}, "\n"))
	writeTestFile(t, filepath.Join(cwd, core.ConfigDirName, "skills", "hidden", "SKILL.md"), strings.Join([]string{
		frontmatterDelimiter,
		"name: hidden",
		"description: Hidden skill",
		"disable-model-invocation: true",
		frontmatterDelimiter,
		"Only explicit calls.",
	}, "\n"))

	result := core.LoadSkills(cwd, agentDir, nil, true)
	require.Empty(t, result.Diagnostics)
	require.Len(t, result.Skills, 2)

	assert.Equal(t, "fix-bug", result.Skills[0].Name)
	assert.Equal(t, core.SourceScopeUser, result.Skills[0].SourceInfo.Scope)
	assert.Equal(t, "hidden", result.Skills[1].Name)
	assert.Equal(t, core.SourceScopeProject, result.Skills[1].SourceInfo.Scope)

	prompt := core.FormatSkillsForPrompt(result.Skills)
	assert.Contains(t, prompt, "<name>fix-bug</name>")
	assert.NotContains(t, prompt, "<name>hidden</name>")
}

func TestLoadSkillsReportsValidationWarningsAndNameCollisions(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	agentDir := t.TempDir()
	writeTestFile(t, filepath.Join(agentDir, "skills", "same", "SKILL.md"), strings.Join([]string{
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

	result := core.LoadSkills(cwd, agentDir, nil, true)
	require.Len(t, result.Skills, 2)
	require.Len(t, result.Diagnostics, 2)

	assert.True(t, hasCollision(result.Diagnostics, "same"))
	assert.True(t, hasDiagnosticMessage(result.Diagnostics, "invalid characters"))
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
