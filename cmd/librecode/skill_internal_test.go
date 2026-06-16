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

	tests := []struct {
		name    string
		want    string
		wantErr string
		args    []string
	}{
		{
			name:    listUse,
			want:    skillCommandName,
			wantErr: "",
			args:    []string{listUse},
		},
		{
			name:    skillCommandShow,
			want:    "description: alpha skill",
			wantErr: "",
			args:    []string{skillCommandShow, "ALPHA"},
		},
		{
			name:    skillCommandValidate,
			want:    "ok",
			wantErr: "",
			args:    []string{skillCommandValidate},
		},
		{
			name:    "show missing",
			want:    "",
			wantErr: `skill "missing" not found`,
			args:    []string{skillCommandShow, "missing"},
		},
	}

	for _, testCase := range tests {
		cmd := newSkillCmd()
		output := new(bytes.Buffer)
		cmd.SetOut(output)
		cmd.SetErr(output)
		cmd.SetArgs(testCase.args)

		err := cmd.Execute()
		if testCase.wantErr != "" {
			require.Error(t, err, testCase.name)
			assert.Contains(t, err.Error(), testCase.wantErr, testCase.name)

			continue
		}

		require.NoError(t, err, testCase.name)
		assert.Contains(t, output.String(), testCase.want, testCase.name)
	}
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
