package core_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/core"
)

const (
	availableSkillsTag = "<available_skills>"
	bashToolName       = "bash"
)

func TestBuildSystemPrompt(t *testing.T) {
	t.Parallel()

	cases := []systemPromptCase{
		defaultSystemPromptCase(),
		customPromptWithoutReadCase(),
		customPromptWithDefaultToolsCase(),
		{
			name:        "nil options use defaults",
			options:     nil,
			contains:    []string{"Available tools:", "Current date:"},
			notContains: []string{},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			prompt := core.BuildSystemPrompt(testCase.options)

			for _, expected := range testCase.contains {
				assert.Contains(t, prompt, expected)
			}
			for _, unexpected := range testCase.notContains {
				assert.NotContains(t, prompt, unexpected)
			}
		})
	}
}

type systemPromptCase struct {
	name        string
	options     *core.BuildSystemPromptOptions
	contains    []string
	notContains []string
}

func defaultSystemPromptCase() systemPromptCase {
	return systemPromptCase{
		name: "default includes selected tools context and skills",
		options: &core.BuildSystemPromptOptions{
			ToolSnippets:       map[string]string{"read": "Read files", bashToolName: "Run commands"},
			CustomPrompt:       "",
			AppendSystemPrompt: "extra instruction",
			CWD:                "/work/project",
			SelectedTools:      []string{"read", bashToolName, "missing"},
			PromptGuidelines:   []string{"  Be safe  ", ""},
			ContextFiles:       []core.ContextFile{{Path: "AGENTS.md", Content: "project rules"}},
			Skills:             []core.Skill{projectPromptSkill("go-test", false)},
		},
		contains: []string{
			"- read: Read files",
			"- bash: Run commands",
			"extra instruction",
			"AGENTS.md",
			"project rules",
			availableSkillsTag,
			"Current working directory: /work/project",
		},
		notContains: []string{"missing:"},
	}
}

func customPromptWithoutReadCase() systemPromptCase {
	return systemPromptCase{
		name: "custom prompt omits skills when read is unavailable",
		options: &core.BuildSystemPromptOptions{
			ToolSnippets:       nil,
			CustomPrompt:       "Custom prompt",
			AppendSystemPrompt: "append",
			CWD:                "/work",
			SelectedTools:      []string{bashToolName},
			PromptGuidelines:   nil,
			ContextFiles:       nil,
			Skills:             []core.Skill{projectPromptSkill("staged-skill", false)},
		},
		contains:    []string{"Custom prompt\n\nappend"},
		notContains: []string{availableSkillsTag},
	}
}

func customPromptWithDefaultToolsCase() systemPromptCase {
	return systemPromptCase{
		name: "custom prompt includes skills when default tools include read",
		options: &core.BuildSystemPromptOptions{
			ToolSnippets:       nil,
			CustomPrompt:       "Custom prompt",
			AppendSystemPrompt: "",
			CWD:                "/work",
			SelectedTools:      nil,
			PromptGuidelines:   nil,
			ContextFiles:       nil,
			Skills:             []core.Skill{projectPromptSkill("staged-skill", false)},
		},
		contains:    []string{availableSkillsTag},
		notContains: []string{},
	}
}

func TestFormatSkillPrompts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		format      func() string
		contains    []string
		notContains []string
	}{
		{
			name: "available skills escape and filter disabled model invocation",
			format: func() string {
				return core.FormatSkillsForPrompt([]core.Skill{
					projectPromptSkill("visible<&>", false),
					projectPromptSkill("staged-skill", true),
				})
			},
			contains:    []string{"visible&lt;&amp;&gt;", "Use &lt;now&gt;"},
			notContains: []string{"staged-skill"},
		},
		{
			name: "active skills escape content and preserve truncation flag",
			format: func() string {
				return core.FormatActiveSkillsForPrompt([]core.ActivatedSkill{{
					Skill:     projectPromptSkill("active<&>", false),
					Content:   "Read <this>",
					Truncated: true,
				}})
			},
			contains:    []string{"active&lt;&amp;&gt;", "<truncated>true</truncated>", "Read &lt;this&gt;"},
			notContains: []string{},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			prompt := testCase.format()

			for _, expected := range testCase.contains {
				assert.Contains(t, prompt, expected)
			}
			for _, unexpected := range testCase.notContains {
				assert.NotContains(t, prompt, unexpected)
			}
		})
	}

	assert.Empty(t, core.FormatSkillsForPrompt(nil))
	assert.Empty(t, core.FormatActiveSkillsForPrompt(nil))
}

func projectPromptSkill(name string, disabled bool) core.Skill {
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
