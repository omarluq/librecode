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
	testSkillSource      = "project"
)

func TestSkillCommands(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	skillPath := filepath.Join(cwd, core.ConfigDirName, "skills", skillCommandName, skillCommandName, "SKILL.md")
	writeCLIFile(t, skillPath, skillCommandMarkdown(skillCommandName))

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

	for i := range tests {
		test := tests[i]
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			cmd := newSkillCmdWithDeps(skillCommandCWDResolver(cwd), skillCommandTestLoader(cwd, skillPath))
			output := new(bytes.Buffer)
			cmd.SetOut(output)
			cmd.SetErr(output)
			cmd.SetArgs(test.args)

			err := cmd.Execute()
			if test.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), test.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Contains(t, output.String(), test.want)
		})
	}
}

func TestFindSkillByNameReturnsMissingSkill(t *testing.T) {
	t.Parallel()

	skill, found := findSkillByName([]core.Skill{}, "missing")

	assert.False(t, found)
	assert.Empty(t, skill.Name)
}

func skillCommandCWDResolver(cwd string) func() (string, error) {
	return func() (string, error) {
		return cwd, nil
	}
}

func skillCommandTestLoader(cwd, skillPath string) skillCommandLoader {
	return func(string) core.LoadSkillsResult {
		return core.LoadSkillsResult{
			Skills:            []core.Skill{skillCommandTestSkill(cwd, skillPath)},
			AgentInstructions: "",
			Diagnostics:       []core.ResourceDiagnostic{},
		}
	}
}

func skillCommandTestSkill(cwd, skillPath string) core.Skill {
	return core.Skill{
		Metadata: nil,
		SourceInfo: core.SourceInfo{
			Path:    skillPath,
			Source:  testSkillSource,
			Scope:   testSkillSource,
			Origin:  testSkillSource,
			BaseDir: cwd,
		},
		Name:                   skillCommandName,
		Description:            "alpha skill",
		FilePath:               skillPath,
		BaseDir:                cwd,
		License:                "",
		Compatibility:          "",
		AllowedTools:           nil,
		UserInvocable:          false,
		DisableModelInvocation: false,
	}
}

func skillCommandMarkdown(name string) string {
	return "---\n" +
		"name: " + name + "\n" +
		"description: " + name + " skill\n" +
		"---\n\n" +
		"# " + name + "\n"
}
