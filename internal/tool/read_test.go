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
	ignoreContent := []byte("secret/\n*.log\n!important.log\n")
	require.NoError(t, os.WriteFile(filepath.Join(workspace, ".gitignore"), ignoreContent, 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(workspace, "secret"), 0o700))
	secretPath := filepath.Join(workspace, "secret", "credentials.txt")
	require.NoError(t, os.WriteFile(secretPath, []byte("super-secret-value"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "debug.log"), []byte("debug"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "important.log"), []byte("important"), 0o600))

	reader := tool.NewReadTool(workspace)
	secretResult, err := reader.Execute(context.Background(), map[string]any{readTestPathKey: "secret/credentials.txt"})
	require.NoError(t, err)
	assert.Contains(t, secretResult.Text(), "Refusing to read ignored path")
	assert.NotContains(t, secretResult.Text(), "super-secret-value")

	logResult, err := reader.Execute(context.Background(), map[string]any{readTestPathKey: "debug.log"})
	require.NoError(t, err)
	assert.Contains(t, logResult.Text(), "Refusing to read ignored path")

	unignoredResult, err := reader.Execute(context.Background(), map[string]any{readTestPathKey: "important.log"})
	require.NoError(t, err)
	assert.Equal(t, "important", unignoredResult.Text())
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
		"allowIgnored":  true,
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
