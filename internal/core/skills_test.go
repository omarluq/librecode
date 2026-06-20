package core_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/core"
)

const (
	hiddenSkillName = "hidden"
	keptSkillName   = "kept-skill"
)

func TestLoadSkillsDiscoversConfiguredRootsAndFormatsPrompt(t *testing.T) {
	cwd := t.TempDir()
	home := t.TempDir()
	agentDir := t.TempDir()
	t.Setenv("HOME", home)

	writeTestFile(t, filepath.Join(
		cwd,
		core.ConfigDirName,
		"skills",
		hiddenSkillName,
		"SKILL.md",
	), strings.Join([]string{
		frontmatterDelimiter,
		"name: " + hiddenSkillName,
		"description: Hidden skill",
		"disable_model_invocation: true",
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
		[]string{hiddenSkillName, "fix-bug", "user-libre", "user-agent"},
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
	assert.NotContains(t, prompt, "<name>"+hiddenSkillName+"</name>")
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
	cases := []struct {
		name         string
		allowedTools []string
		wantTools    []string
	}{
		{
			name:         "space separated allowed tools",
			allowedTools: []string{"allowed_tools: Bash(git:*) Read"},
			wantTools:    []string{"Bash(git:*)", "Read"},
		},
		{
			name: "sequence allowed tools trims blank entries",
			allowedTools: []string{
				"allowed_tools:",
				"  - Bash(git:*)",
				"  - '  Read  '",
				"  - ''",
			},
			wantTools: []string{"Bash(git:*)", "Read"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			cwd := t.TempDir()
			home := t.TempDir()
			t.Setenv("HOME", home)

			frontmatter := []string{
				frontmatterDelimiter,
				"name: spec-skill",
				"description: Use for spec parsing",
				"license: MIT",
				"compatibility: Works with librecode",
			}
			frontmatter = append(frontmatter, testCase.allowedTools...)
			frontmatter = append(frontmatter,
				"user_invocable: true",
				"metadata:",
				"  author: omar",
				frontmatterDelimiter,
				"Body.",
			)
			writeTestFile(
				t,
				filepath.Join(cwd, core.ConfigDirName, "skills", "spec-skill", "SKILL.md"),
				strings.Join(frontmatter, "\n"),
			)

			result := core.LoadSkills(cwd, nil, true)

			require.Empty(t, result.Diagnostics)
			require.Len(t, result.Skills, 1)
			skill := result.Skills[0]
			assert.Equal(t, "MIT", skill.License)
			assert.Equal(t, "Works with librecode", skill.Compatibility)
			assert.Equal(t, testCase.wantTools, skill.AllowedTools)
			assert.True(t, skill.UserInvocable)
			assert.Equal(t, "omar", skill.Metadata["author"])
			content, err := core.SkillContent(&skill)
			require.NoError(t, err)
			assert.Contains(t, content, "spec-skill")
		})
	}
}

func TestLoadSkillsHonorsIgnoreFiles(t *testing.T) {
	testCases := []struct {
		name        string
		ignoreFile  string
		want        []string
		writeDirect bool
		writeNested bool
	}{
		{
			name:        "ignores skill directory",
			ignoreFile:  "ignored-skill\n",
			want:        []string{keptSkillName},
			writeDirect: true,
			writeNested: false,
		},
		{
			name:        "carries parent patterns into nested directories",
			ignoreFile:  "nested/ignored-skill\n",
			want:        []string{keptSkillName},
			writeDirect: false,
			writeNested: true,
		},
		{
			name:        "supports negated patterns",
			ignoreFile:  "ignored-skill\nnested/*\n!nested/ignored-skill/\n",
			want:        []string{keptSkillName, "ignored-skill"},
			writeDirect: true,
			writeNested: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			cwd := t.TempDir()
			home := t.TempDir()
			t.Setenv("HOME", home)

			skillsDir := filepath.Join(cwd, core.ConfigDirName, "skills")
			writeTestFile(t, filepath.Join(skillsDir, ".ignore"), testCase.ignoreFile)

			if testCase.writeDirect {
				writeTestFile(t, filepath.Join(skillsDir, "ignored-skill", "SKILL.md"), skillMarkdown("ignored-skill"))
			}

			if testCase.writeNested {
				nestedPath := filepath.Join(skillsDir, "nested", "ignored-skill", "SKILL.md")
				writeTestFile(t, nestedPath, skillMarkdown("ignored-skill"))
			}

			writeTestFile(t, filepath.Join(skillsDir, keptSkillName, "SKILL.md"), skillMarkdown(keptSkillName))

			result := core.LoadSkills(cwd, nil, true)

			require.Empty(t, result.Diagnostics)
			assert.Equal(t, testCase.want, skillNames(result.Skills))
		})
	}
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

func TestFormatActiveSkillsForPrompt(t *testing.T) {
	t.Parallel()

	prompt := core.FormatActiveSkillsForPrompt([]core.ActivatedSkill{
		{
			Skill:     activePromptSkill("fix<&>", "/tmp/SKILL.md"),
			Content:   "Use <unsafe> & carefully",
			Truncated: true,
		},
	})

	assert.Contains(t, prompt, "<active_skills>")
	assert.Contains(t, prompt, "<name>fix&lt;&amp;&gt;</name>")
	assert.Contains(t, prompt, "<truncated>true</truncated>")
	assert.Contains(t, prompt, "Use &lt;unsafe&gt; &amp; carefully")
}

func activePromptSkill(name, filePath string) core.Skill {
	return core.Skill{
		Metadata: nil,
		SourceInfo: core.SourceInfo{
			Path:    "",
			Source:  "",
			Scope:   "",
			Origin:  "",
			BaseDir: "",
		},
		Name:                   name,
		Description:            "",
		FilePath:               filePath,
		BaseDir:                "",
		License:                "",
		Compatibility:          "",
		AllowedTools:           nil,
		UserInvocable:          false,
		DisableModelInvocation: false,
	}
}
