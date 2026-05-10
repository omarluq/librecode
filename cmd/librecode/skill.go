package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/core"
)

func newSkillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "List and inspect Agent Skills",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return newSkillListCmd().RunE(cmd, nil)
		},
	}

	cmd.AddCommand(newSkillListCmd())
	cmd.AddCommand(newSkillShowCmd())
	cmd.AddCommand(newSkillValidateCmd())

	return cmd
}

func newSkillListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   listUse,
		Short: "List discovered Agent Skills",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd, err := assistant.DefaultCWD("")
			if err != nil {
				return err
			}
			result := core.LoadSkills(cwd, nil, true)
			for index := range result.Skills {
				skill := &result.Skills[index]
				if err := printLine(
					cmd.OutOrStdout(),
					"%s\t%s\t%s",
					skill.Name,
					skill.FilePath,
					skill.Description,
				); err != nil {
					return err
				}
			}

			return nil
		},
	}
}

func newSkillShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "Print one skill's SKILL.md content",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := assistant.DefaultCWD("")
			if err != nil {
				return err
			}
			skill, found := findSkillByName(cwd, args[0])
			if !found {
				return fmt.Errorf("skill %q not found", args[0])
			}
			content, err := core.SkillContent(&skill)
			if err != nil {
				return err
			}

			return printLine(cmd.OutOrStdout(), "%s", content)
		},
	}
}

func newSkillValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate discovered Agent Skills",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd, err := assistant.DefaultCWD("")
			if err != nil {
				return err
			}
			result := core.LoadSkills(cwd, nil, true)
			for index := range result.Diagnostics {
				diagnostic := &result.Diagnostics[index]
				if err := printLine(
					cmd.OutOrStdout(),
					"%s\t%s\t%s",
					diagnostic.Type,
					diagnostic.Path,
					diagnostic.Message,
				); err != nil {
					return err
				}
			}
			if len(result.Diagnostics) > 0 {
				return fmt.Errorf("skills validation reported %d diagnostic(s)", len(result.Diagnostics))
			}

			return printLine(cmd.OutOrStdout(), "ok")
		},
	}
}

func findSkillByName(cwd, name string) (core.Skill, bool) {
	result := core.LoadSkills(cwd, nil, true)
	for index := range result.Skills {
		skill := result.Skills[index]
		if strings.EqualFold(skill.Name, name) {
			return skill, true
		}
	}

	return core.Skill{
		Metadata: nil,
		SourceInfo: core.SourceInfo{
			Path:    "",
			Source:  "",
			Scope:   "",
			Origin:  "",
			BaseDir: "",
		},
		Name:                   "",
		Description:            "",
		FilePath:               "",
		BaseDir:                "",
		License:                "",
		Compatibility:          "",
		AllowedTools:           nil,
		UserInvocable:          false,
		DisableModelInvocation: false,
	}, false
}
