package core_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/core"
)

const bashToolName = "bash"

func TestBuildSystemPromptDefaultIncludesToolsContextAndSkills(t *testing.T) {
	t.Parallel()

	prompt := core.BuildSystemPrompt(&core.BuildSystemPromptOptions{
		ToolSnippets:       map[string]string{"read": "Read files", bashToolName: "Run commands"},
		CustomPrompt:       "",
		AppendSystemPrompt: "extra instruction",
		CWD:                "/work/project",
		SelectedTools:      []string{"read", bashToolName, "missing"},
		PromptGuidelines:   []string{"  Be safe  ", ""},
		ContextFiles:       []core.ContextFile{{Path: "AGENTS.md", Content: "project rules"}},
		Skills: []core.Skill{{
			Metadata:               nil,
			SourceInfo:             core.SourceInfo{Path: "", Source: "", Scope: "", Origin: "", BaseDir: ""},
			Name:                   "go-test",
			Description:            "Use when writing tests",
			FilePath:               "/skills/go-test/SKILL.md",
			BaseDir:                "/skills/go-test",
			License:                "",
			Compatibility:          "",
			AllowedTools:           nil,
			UserInvocable:          false,
			DisableModelInvocation: false,
		}},
	})

	assert.Contains(t, prompt, "- read: Read files")
	assert.Contains(t, prompt, "- bash: Run commands")
	assert.NotContains(t, prompt, "missing:")
	assert.Contains(t, prompt, "extra instruction")
	assert.Contains(t, prompt, "AGENTS.md")
	assert.Contains(t, prompt, "project rules")
	assert.Contains(t, prompt, "<available_skills>")
	assert.Contains(t, prompt, "Current working directory: /work/project")
}

func TestBuildSystemPromptCustomCanOmitSkillsWithoutRead(t *testing.T) {
	t.Parallel()

	skills := []core.Skill{testPromptSkill("staged-skill", false)}
	withoutRead := core.BuildSystemPrompt(&core.BuildSystemPromptOptions{
		ToolSnippets:       nil,
		CustomPrompt:       "Custom prompt",
		AppendSystemPrompt: "append",
		CWD:                "/work",
		SelectedTools:      []string{bashToolName},
		PromptGuidelines:   nil,
		ContextFiles:       nil,
		Skills:             skills,
	})
	assert.Contains(t, withoutRead, "Custom prompt\n\nappend")
	assert.NotContains(t, withoutRead, "<available_skills>")

	withDefaultTools := core.BuildSystemPrompt(&core.BuildSystemPromptOptions{
		ToolSnippets:       nil,
		CustomPrompt:       "Custom prompt",
		AppendSystemPrompt: "",
		CWD:                "/work",
		SelectedTools:      nil,
		PromptGuidelines:   nil,
		ContextFiles:       nil,
		Skills:             skills,
	})
	assert.Contains(t, withDefaultTools, "<available_skills>")
}

func TestBuildSystemPromptNilOptions(t *testing.T) {
	t.Parallel()

	prompt := core.BuildSystemPrompt(nil)

	assert.Contains(t, prompt, "Available tools:")
	assert.Contains(t, prompt, "Current date:")
}

func TestFormatSkillsForPromptEscapesAndFilters(t *testing.T) {
	t.Parallel()

	prompt := core.FormatSkillsForPrompt([]core.Skill{
		testPromptSkill("visible<&>", false),
		testPromptSkill("staged-skill", true),
	})

	assert.Contains(t, prompt, "visible&lt;&amp;&gt;")
	assert.Contains(t, prompt, "Use &lt;now&gt;")
	assert.NotContains(t, prompt, "staged-skill")
	assert.Empty(t, core.FormatSkillsForPrompt(nil))
}

func TestFormatActiveSkillsForPrompt(t *testing.T) {
	t.Parallel()

	prompt := core.FormatActiveSkillsForPrompt([]core.ActivatedSkill{{
		Skill:     testPromptSkill("active<&>", false),
		Content:   "Read <this>",
		Truncated: true,
	}})

	assert.Contains(t, prompt, "active&lt;&amp;&gt;")
	assert.Contains(t, prompt, "<truncated>true</truncated>")
	assert.Contains(t, prompt, "Read &lt;this&gt;")
	assert.Empty(t, core.FormatActiveSkillsForPrompt(nil))
}

func testPromptSkill(name string, disabled bool) core.Skill {
	return core.Skill{
		Metadata:               nil,
		SourceInfo:             core.SourceInfo{Path: "", Source: "", Scope: "", Origin: "", BaseDir: ""},
		Name:                   name,
		Description:            "Use <now>",
		FilePath:               "/skill/SKILL.md",
		BaseDir:                "/skill",
		License:                "",
		Compatibility:          "",
		AllowedTools:           nil,
		UserInvocable:          false,
		DisableModelInvocation: disabled,
	}
}
