package core

import (
	"fmt"
	"html"
	"strings"

	"github.com/samber/lo"
)

// FormatSkillsForPrompt formats skill metadata in librecode's XML prompt block.
func FormatSkillsForPrompt(skills []Skill) string {
	visibleSkills := lo.Filter(skills, func(skill Skill, _ int) bool {
		return !skill.DisableModelInvocation
	})
	if len(visibleSkills) == 0 {
		return ""
	}

	lines := []string{
		"\n\nThe following skills provide specialized instructions for specific tasks.",
		"Use the read tool to load a skill's file when the task matches its description.",
		"When a skill file references a relative path, resolve it against the skill directory " +
			"(parent of SKILL.md / dirname of the path) and use that absolute path in tool commands.",
		"",
		"<available_skills>",
	}
	for index := range visibleSkills {
		skill := &visibleSkills[index]
		lines = append(lines,
			"  <skill>",
			fmt.Sprintf("    <name>%s</name>", html.EscapeString(skill.Name)),
			fmt.Sprintf("    <description>%s</description>", html.EscapeString(skill.Description)),
			fmt.Sprintf("    <location>%s</location>", html.EscapeString(skill.FilePath)),
			"  </skill>",
		)
	}
	lines = append(lines, "</available_skills>")

	return strings.Join(lines, "\n")
}

// FormatActiveSkillsForPrompt formats full activated skill content for the model request.
func FormatActiveSkillsForPrompt(skills []ActivatedSkill) string {
	if len(skills) == 0 {
		return ""
	}

	lines := []string{
		"\n\nThe following skills were automatically activated for this request.",
		"Follow their instructions when relevant.",
		"",
		"<active_skills>",
	}
	for index := range skills {
		activation := &skills[index]
		lines = append(lines,
			"  <skill>",
			fmt.Sprintf("    <name>%s</name>", html.EscapeString(activation.Skill.Name)),
			fmt.Sprintf("    <location>%s</location>", html.EscapeString(activation.Skill.FilePath)),
		)
		if activation.Truncated {
			lines = append(lines, "    <truncated>true</truncated>")
		}
		lines = append(lines,
			"    <content>",
			html.EscapeString(activation.Content),
			"    </content>",
			"  </skill>",
		)
	}
	lines = append(lines, "</active_skills>")

	return strings.Join(lines, "\n")
}
