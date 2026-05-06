package core

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/samber/lo"

	"github.com/omarluq/librecode/internal/tool"
)

const (
	systemPromptIntro = "You are an expert coding assistant operating inside librecode. " +
		"You help users by reading files, executing commands, editing code, and writing new files."
	documentationHeader = "librecode documentation (read only when the user asks about librecode itself, " +
		"its extensions, themes, skills, or TUI):"
)

// ContextFile is project context appended to the system prompt.
type ContextFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// BuildSystemPromptOptions controls librecode-style system prompt construction.
type BuildSystemPromptOptions struct {
	ToolSnippets       map[string]string `json:"tool_snippets"`
	CustomPrompt       string            `json:"custom_prompt"`
	AppendSystemPrompt string            `json:"append_system_prompt"`
	CWD                string            `json:"cwd"`
	SelectedTools      []string          `json:"selected_tools"`
	PromptGuidelines   []string          `json:"prompt_guidelines"`
	ContextFiles       []ContextFile     `json:"context_files"`
	Skills             []Skill           `json:"skills"`
}

// BuildSystemPrompt builds the default librecode coding-agent prompt in Go.
func BuildSystemPrompt(options *BuildSystemPromptOptions) string {
	if options == nil {
		options = &BuildSystemPromptOptions{
			ToolSnippets:       nil,
			CustomPrompt:       "",
			AppendSystemPrompt: "",
			CWD:                "",
			SelectedTools:      nil,
			PromptGuidelines:   nil,
			ContextFiles:       nil,
			Skills:             nil,
		}
	}
	promptDate := time.Now().Format(time.DateOnly)
	promptCWD := filepath.ToSlash(options.CWD)
	appendSection := ""
	if options.AppendSystemPrompt != "" {
		appendSection = "\n\n" + options.AppendSystemPrompt
	}
	if options.CustomPrompt != "" {
		return buildCustomSystemPrompt(options, appendSection, promptDate, promptCWD)
	}

	selectedTools := selectedPromptTools(options.SelectedTools)
	toolSnippets := options.ToolSnippets
	if len(toolSnippets) == 0 {
		toolSnippets = defaultToolSnippets()
	}

	prompt := defaultSystemPrompt(selectedTools, toolSnippets, options.PromptGuidelines) + appendSection
	prompt += formatContextFiles(options.ContextFiles)
	if lo.Contains(selectedTools, string(tool.NameRead)) && len(options.Skills) > 0 {
		prompt += FormatSkillsForPrompt(options.Skills)
	}
	prompt += "\nCurrent date: " + promptDate
	prompt += "\nCurrent working directory: " + promptCWD

	return prompt
}

func selectedPromptTools(selectedTools []string) []string {
	if len(selectedTools) != 0 {
		return selectedTools
	}

	return []string{
		string(tool.NameRead),
		string(tool.NameBash),
		string(tool.NameEdit),
		string(tool.NameWrite),
	}
}

func buildCustomSystemPrompt(
	options *BuildSystemPromptOptions,
	appendSection string,
	promptDate string,
	promptCWD string,
) string {
	prompt := options.CustomPrompt + appendSection
	prompt += formatContextFiles(options.ContextFiles)
	if customPromptHasRead(options.SelectedTools) && len(options.Skills) > 0 {
		prompt += FormatSkillsForPrompt(options.Skills)
	}
	prompt += "\nCurrent date: " + promptDate
	prompt += "\nCurrent working directory: " + promptCWD

	return prompt
}

func customPromptHasRead(selectedTools []string) bool {
	return len(selectedTools) == 0 || lo.Contains(selectedTools, string(tool.NameRead))
}

func defaultSystemPrompt(selectedTools []string, snippets map[string]string, extraGuidelines []string) string {
	sections := []string{
		systemPromptIntro,
		"",
		"Available tools:",
		formatToolsList(selectedTools, snippets),
		"",
		"In addition to the tools above, you may have access to other custom tools depending on the project.",
		"",
		"Guidelines:",
		formatGuidelines(selectedTools, extraGuidelines),
		"",
		documentationHeader,
		"- Main documentation: README.md",
		"- Additional docs: docs",
		"- Examples: examples (extensions, custom tools, SDK)",
		"- When asked about: extensions (docs/extensions.md, examples/extensions/), themes (docs/themes.md), " +
			"skills (docs/skills.md), prompt templates (docs/prompt-templates.md), TUI components (docs/tui.md), " +
			"keybindings (docs/keybindings.md), SDK integrations (docs/sdk.md), " +
			"custom providers (docs/custom-provider.md), adding models (docs/models.md), " +
			"packages (docs/packages.md)",
		"- When working on librecode topics, read the docs and examples, and follow .md cross-references " +
			"before implementing",
		"- Always read librecode .md files completely and follow links to related docs " +
			"(e.g., tui.md for TUI API details)",
	}

	return strings.Join(sections, "\n")
}

func formatToolsList(selectedTools []string, snippets map[string]string) string {
	visibleTools := lo.Filter(selectedTools, func(name string, _ int) bool {
		return snippets[name] != ""
	})
	if len(visibleTools) == 0 {
		return "(none)"
	}
	lines := lo.Map(visibleTools, func(name string, _ int) string {
		return "- " + name + ": " + snippets[name]
	})

	return strings.Join(lines, "\n")
}

func formatGuidelines(selectedTools, extraGuidelines []string) string {
	guidelines := []string{}
	guidelines = appendToolGuidelines(guidelines, selectedTools)
	guidelines = append(guidelines, lo.FilterMap(extraGuidelines, func(guideline string, _ int) (string, bool) {
		trimmed := strings.TrimSpace(guideline)
		return trimmed, trimmed != ""
	})...)
	guidelines = append(guidelines,
		"Be concise in your responses",
		"Show file paths clearly when working with files",
	)
	guidelines = lo.Uniq(guidelines)

	return strings.Join(lo.Map(guidelines, func(guideline string, _ int) string {
		return "- " + guideline
	}), "\n")
}

func appendToolGuidelines(guidelines, selectedTools []string) []string {
	hasBash := lo.Contains(selectedTools, string(tool.NameBash))
	hasReadOnlySearch := lo.SomeBy(selectedTools, func(name string) bool {
		return lo.Contains([]string{string(tool.NameGrep), string(tool.NameFind), string(tool.NameLS)}, name)
	})
	switch {
	case hasBash && !hasReadOnlySearch:
		return append(guidelines, "Use bash for file operations like ls, rg, find")
	case hasBash && hasReadOnlySearch:
		return append(guidelines,
			"Prefer grep/find/ls tools over bash for file exploration (faster, respects .gitignore)",
		)
	default:
		return guidelines
	}
}

func formatContextFiles(contextFiles []ContextFile) string {
	if len(contextFiles) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("\n\n# Project Context\n\nProject-specific instructions and guidelines:\n\n")
	for _, contextFile := range contextFiles {
		builder.WriteString("## ")
		builder.WriteString(contextFile.Path)
		builder.WriteString("\n\n")
		builder.WriteString(contextFile.Content)
		builder.WriteString("\n\n")
	}

	return builder.String()
}

func defaultToolSnippets() map[string]string {
	return lo.SliceToMap(tool.AllDefinitions(), func(definition tool.Definition) (string, string) {
		return string(definition.Name), definition.PromptSnippet
	})
}
