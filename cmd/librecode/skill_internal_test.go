package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/core"
)

const (
	skillCommandName     = "alpha"
	skillCommandShow     = "show"
	skillCommandValidate = "validate"
)

func TestSkillCommands(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LIBRECODE_HOME", home)
	writeCLIFile(
		t,
		filepath.Join(cwd, core.ConfigDirName, "skills", skillCommandName, "SKILL.md"),
		skillCommandMarkdown(skillCommandName),
	)
	t.Chdir(cwd)

	runSkillCommandCase := func(name string, args []string, want, wantErr string) {
		t.Run(name, func(t *testing.T) {
			cmd := newSkillCmd()
			output := new(bytes.Buffer)
			cmd.SetOut(output)
			cmd.SetErr(output)
			cmd.SetArgs(args)

			err := cmd.Execute()
			if wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), wantErr)

				return
			}

			require.NoError(t, err)
			assert.Contains(t, output.String(), want)
		})
	}

	runSkillCommandCase(listUse, []string{listUse}, skillCommandName, "")
	runSkillCommandCase(skillCommandShow, []string{skillCommandShow, "ALPHA"}, "description: alpha skill", "")
	runSkillCommandCase(skillCommandValidate, []string{skillCommandValidate}, "ok", "")
	runSkillCommandCase("show missing", []string{skillCommandShow, "missing"}, "", `skill "missing" not found`)
}

func TestFindSkillByNameReturnsMissingSkill(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LIBRECODE_HOME", home)

	skill, found := findSkillByName(cwd, "missing")

	assert.False(t, found)
	assert.Empty(t, skill.Name)
}

func skillCommandMarkdown(name string) string {
	return "---\n" +
		"name: " + name + "\n" +
		"description: " + name + " skill\n" +
		"---\n\n" +
		"# " + name + "\n"
}
