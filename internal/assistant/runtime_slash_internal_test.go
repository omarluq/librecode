package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/core"
	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/tool"
)

const (
	runtimeSlashHelloPrompt = "/hello world"
	runtimeSlashMissing     = "missing"
)

func TestRespondToSlashCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		extensions   runtimeExtensions
		prompt       string
		name         string
		wantResponse string
		wantErrText  string
	}{
		{
			extensions:   nil,
			prompt:       "/   ",
			name:         "empty slash command",
			wantResponse: "",
			wantErrText:  "empty slash command",
		},
		{
			extensions:   slashCommandExtensions{err: nil, response: "hi world"},
			prompt:       runtimeSlashHelloPrompt,
			name:         "extension command",
			wantResponse: "hi world",
			wantErrText:  "",
		},
		{
			extensions:   slashCommandExtensions{err: errors.New("boom"), response: ""},
			prompt:       runtimeSlashHelloPrompt,
			name:         "extension command error",
			wantResponse: "",
			wantErrText:  "execute command",
		},
		{
			extensions:   slashCommandExtensions{err: nil, response: ""},
			prompt:       runtimeSlashHelloPrompt,
			name:         "missing extension command",
			wantResponse: "",
			wantErrText:  "execute command",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			runtime := newRuntimeFromDeps(func(deps *runtimeDeps) {
				deps.Extensions = testCase.extensions
			})

			response, toolEvents, err := runtime.respondToSlashCommand(
				context.Background(),
				t.TempDir(),
				testCase.prompt,
				nil,
			)
			if testCase.wantErrText != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.wantErrText)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.wantResponse, response)
			assert.Empty(t, toolEvents)
		})
	}
}

func TestRespondToSkillCommand(t *testing.T) {
	t.Run("lists skills", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		root := t.TempDir()
		skillPath := writeSlashTestSkill(t, root, "one", "First skill", "skill body")
		runtime := newRuntimeFromDeps(nil)

		response, events, err := runtime.respondToSkillCommand(context.Background(), root, "", nil)

		require.NoError(t, err)
		assert.Contains(t, response, "Available skills:")
		assert.Contains(t, response, "- one: First skill")
		assert.Empty(t, events)
		assert.FileExists(t, skillPath)
	})

	t.Run("loads named skill and emits event", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		root := t.TempDir()
		writeSlashTestSkill(t, root, "one", "First skill", "skill body")

		runtime := newRuntimeFromDeps(nil)
		streamEvents := []StreamEvent{}

		response, toolEvents, err := runtime.respondToSkillCommand(
			context.Background(),
			root,
			"one",
			func(event StreamEvent) { streamEvents = append(streamEvents, event) },
		)

		require.NoError(t, err)
		assert.Contains(t, response, "skill body")
		require.Len(t, toolEvents, 1)
		assert.Equal(t, "load skill: one", toolEvents[0].Name)
		require.Len(t, streamEvents, 1)
		assert.Equal(t, StreamEventSkillLoaded, streamEvents[0].Kind)
	})

	t.Run("unknown skill", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		root := t.TempDir()
		writeSlashTestSkill(t, root, "one", "First skill", "skill body")

		runtime := newRuntimeFromDeps(nil)

		_, _, err := runtime.respondToSkillCommand(context.Background(), root, "two", nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), `skill "two" not found`)
	})
}

func TestRespondToSkillCommandWithNoSkills(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	response, events, err := newRuntimeFromDeps(nil).respondToSkillCommand(context.Background(), t.TempDir(), "", nil)

	require.NoError(t, err)
	assert.Equal(t, "No skills found.", response)
	assert.Empty(t, events)
}

func TestLoadSkillWithReadToolReportsReadErrors(t *testing.T) {
	t.Parallel()

	skill := runtimeSlashSkill(filepath.Join(t.TempDir(), runtimeSlashMissing, "SKILL.md"))

	_, toolEvent, err := newRuntimeFromDeps(nil).loadSkillWithReadTool(context.Background(), t.TempDir(), skill, nil)

	require.Error(t, err)
	assert.True(t, toolEvent.IsError)
	assert.Contains(t, toolEvent.Error, "read file")
}

func TestLoadSkillWithReadToolUsesLimit(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	skillPath := writeSlashTestSkill(t, root, "limited", "Limited skill", "one\ntwo")
	limit := 1

	content, event, err := newRuntimeFromDeps(nil).loadSkillWithReadTool(
		context.Background(),
		root,
		runtimeSlashSkill(skillPath),
		&limit,
	)

	require.NoError(t, err)
	expectedArguments, err := json.Marshal(struct {
		Path  string `json:"path"`
		Limit int    `json:"limit"`
	}{
		Path:  skillPath,
		Limit: limit,
	})
	require.NoError(t, err)

	assert.Contains(t, content, "Use offset=2 to continue")
	assert.NotContains(t, content, "two")
	assert.JSONEq(t, string(expectedArguments), event.ArgumentsJSON)
}

func TestRespondToToolCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		args         string
		name         string
		wantResponse string
		wantErrText  string
	}{
		{
			args:         " ",
			name:         "missing tool name",
			wantResponse: "",
			wantErrText:  "tool command requires a tool name",
		},
		{
			args:         "ls",
			name:         "default empty payload",
			wantResponse: "(empty directory)",
			wantErrText:  "",
		},
		{
			args:         "ls {",
			name:         "invalid payload",
			wantResponse: "",
			wantErrText:  "execute built-in tool",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			response, err := newRuntimeFromDeps(nil).respondToToolCommand(
				context.Background(),
				t.TempDir(),
				testCase.args,
			)
			if testCase.wantErrText != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.wantErrText)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.wantResponse, response)
		})
	}
}

func TestSkillReadArgumentsJSON(t *testing.T) {
	t.Parallel()

	limit := 5
	tests := []struct {
		limit *int
		name  string
		want  string
	}{
		{limit: nil, name: "without limit", want: `{"path":"/tmp/SKILL.md"}`},
		{limit: &limit, name: "with limit", want: `{"limit":5,"path":"/tmp/SKILL.md"}`},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			payload, err := skillReadArgumentsJSON("/tmp/SKILL.md", testCase.limit)

			require.NoError(t, err)
			assert.JSONEq(t, testCase.want, string(payload))
		})
	}
}

func writeSlashTestSkill(t *testing.T, root, name, description, body string) string {
	t.Helper()

	skillDir := filepath.Join(root, core.ConfigDirName, "skills", name)
	require.NoError(t, os.MkdirAll(skillDir, 0o700))
	skillPath := filepath.Join(skillDir, "SKILL.md")
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n" + body
	require.NoError(t, os.WriteFile(skillPath, []byte(content), 0o600))

	return skillPath
}

func runtimeSlashSkill(path string) *core.Skill {
	return &core.Skill{
		Metadata: nil,
		SourceInfo: core.NewSourceInfo("", core.SourceInfoOptions{
			Scope:   "",
			Origin:  "",
			BaseDir: "",
			Source:  "",
		}),
		Name:                   runtimeSlashMissing,
		Description:            "",
		FilePath:               path,
		BaseDir:                "",
		License:                "",
		Compatibility:          "",
		AllowedTools:           nil,
		UserInvocable:          false,
		DisableModelInvocation: false,
	}
}

type slashCommandExtensions struct {
	err      error
	response string
}

func (extensions slashCommandExtensions) ExecuteCommand(context.Context, string, string) (string, error) {
	if extensions.response == "" && extensions.err == nil {
		return "", errors.New("not found")
	}

	return extensions.response, extensions.err
}

func (slashCommandExtensions) Emit(context.Context, string, map[string]any) error { return nil }

func (slashCommandExtensions) ExecuteTool(context.Context, string, tool.Arguments) (extension.ToolResult, error) {
	return extension.ToolResult{Details: nil, Content: ""}, nil
}

func (slashCommandExtensions) Tools() []extension.Tool { return nil }

func (slashCommandExtensions) DispatchLifecycle(
	_ context.Context,
	event extension.LifecycleEvent,
) (extension.LifecycleDispatchResult, error) {
	return emptyTestLifecycleDispatchResult(event), nil
}
