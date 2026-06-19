package tool_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/tool"
)

const readTestPathKey = "path"

func TestReadToolRespectsGitignoreByDefault(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	ignoreContent := []byte("secret/\n*.log\n!important.log\nnested/.env\n")
	require.NoError(t, os.WriteFile(filepath.Join(workspace, ".gitignore"), ignoreContent, 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(workspace, "secret"), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(workspace, "nested"), 0o700))
	secretPath := filepath.Join(workspace, "secret", "credentials.txt")
	require.NoError(t, os.WriteFile(secretPath, []byte("super-secret-value"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "debug.log"), []byte("debug"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "important.log"), []byte("important"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "nested", ".env"), []byte("nested secret"), 0o600))

	reader := tool.NewReadTool(workspace)
	tests := []struct {
		name         string
		path         string
		wantText     string
		wantRedacted string
		wantRefusal  bool
	}{
		{
			name:         "directory pattern blocks descendant",
			path:         "secret/credentials.txt",
			wantText:     "",
			wantRedacted: "super-secret-value",
			wantRefusal:  true,
		},
		{
			name:         "glob pattern blocks file",
			path:         "debug.log",
			wantText:     "",
			wantRedacted: "",
			wantRefusal:  true,
		},
		{
			name:         "negated pattern allows file",
			path:         "important.log",
			wantText:     "important",
			wantRedacted: "",
			wantRefusal:  false,
		},
		{
			name:         "slash pattern matches from root",
			path:         "nested/.env",
			wantText:     "",
			wantRedacted: "",
			wantRefusal:  true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result, err := reader.Execute(context.Background(), map[string]any{readTestPathKey: testCase.path})
			require.NoError(t, err)

			if testCase.wantRefusal {
				assert.Contains(t, result.Text(), "Refusing to read ignored path")

				if testCase.wantRedacted != "" {
					assert.NotContains(t, result.Text(), testCase.wantRedacted)
				}

				return
			}

			assert.Equal(t, testCase.wantText, result.Text())
		})
	}
}

func TestReadToolAllowsExplicitIgnoredReads(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workspace, ".env"), []byte("SECRET=value"), 0o600))

	reader := tool.NewReadTool(workspace)
	blockedResult, err := reader.Execute(context.Background(), map[string]any{readTestPathKey: ".env"})
	require.NoError(t, err)
	assert.Contains(t, blockedResult.Text(), "Refusing to read ignored path")

	allowedResult, err := reader.Execute(context.Background(), map[string]any{
		"allow_ignored": true,
		readTestPathKey: ".env",
	})
	require.NoError(t, err)
	assert.Equal(t, "SECRET=value", allowedResult.Text())
}

func TestReadToolAllowsAgentSkillFilesByDefault(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	skillPath := filepath.Join(workspace, ".agents", "skills", "fix-bug", "SKILL.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(skillPath), 0o700))
	require.NoError(t, os.WriteFile(skillPath, []byte("skill body"), 0o600))

	reader := tool.NewReadTool(workspace)
	result, err := reader.Execute(context.Background(), map[string]any{
		readTestPathKey: filepath.Join(".agents", "skills", "fix-bug", "SKILL.md"),
	})
	require.NoError(t, err)
	assert.Equal(t, "skill body", result.Text())
}
