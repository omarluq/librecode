package tool_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

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

			result, err := reader.Execute(
				context.Background(),
				testToolArguments(map[string]any{readTestPathKey: testCase.path}),
			)
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

func TestReadToolInvalidatesGitignoreCache(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setup  func(t *testing.T, workspace string)
		change func(t *testing.T, workspace string)
		name   string
	}{
		{
			name: "when existing gitignore changes",
			setup: func(t *testing.T, workspace string) {
				t.Helper()
				writeReadTestFile(t, filepath.Join(workspace, ".gitignore"), "")
			},
			change: func(t *testing.T, workspace string) {
				t.Helper()
				writeReadTestFile(t, filepath.Join(workspace, ".gitignore"), "*.secret\n")
			},
		},
		{
			name: "when nested gitignore is added",
			setup: func(t *testing.T, workspace string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Join(workspace, "nested"), 0o700))
			},
			change: func(t *testing.T, workspace string) {
				t.Helper()
				writeReadTestFile(t, filepath.Join(workspace, "nested", ".gitignore"), "*.secret\n")
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			workspace := t.TempDir()
			referenceTime := time.Now().Add(time.Hour)
			relativePath := filepath.ToSlash(filepath.Join("nested", "later.secret"))
			ignoredPath := filepath.Join(workspace, "nested", "later.secret")
			require.NoError(t, os.MkdirAll(filepath.Dir(ignoredPath), 0o700))
			writeReadTestFile(t, ignoredPath, "secret")
			testCase.setup(t, workspace)
			setReadTestModTime(t, workspace, referenceTime)

			reader := tool.NewReadTool(workspace)
			initialResult, err := reader.Execute(
				t.Context(),
				testToolArguments(map[string]any{readTestPathKey: relativePath}),
			)
			require.NoError(t, err)
			assert.Equal(t, "secret", initialResult.Text())

			testCase.change(t, workspace)
			setReadTestModTime(t, workspace, referenceTime.Add(time.Hour))

			changedResult, err := reader.Execute(
				t.Context(),
				testToolArguments(map[string]any{readTestPathKey: relativePath}),
			)
			require.NoError(t, err)
			assert.Contains(t, changedResult.Text(), "Refusing to read ignored path")
		})
	}
}

func writeReadTestFile(t *testing.T, path, content string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func setReadTestModTime(t *testing.T, root string, modTime time.Time) {
	t.Helper()

	entries, err := os.ReadDir(root)
	require.NoError(t, err)

	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		require.NoError(t, os.Chtimes(path, modTime, modTime))

		if entry.IsDir() {
			setReadTestModTime(t, path, modTime)
		}
	}

	require.NoError(t, os.Chtimes(root, modTime, modTime))
}

func TestReadToolAllowsExplicitIgnoredReads(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workspace, ".env"), []byte("SECRET=value"), 0o600))

	reader := tool.NewReadTool(workspace)
	blockedResult, err := reader.Execute(
		context.Background(),
		testToolArguments(map[string]any{readTestPathKey: ".env"}),
	)
	require.NoError(t, err)
	assert.Contains(t, blockedResult.Text(), "Refusing to read ignored path")

	allowedResult, err := reader.Execute(context.Background(), testToolArguments(map[string]any{
		"allow_ignored": true,
		readTestPathKey: ".env",
	}))
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
	result, err := reader.Execute(context.Background(), testToolArguments(map[string]any{
		readTestPathKey: filepath.Join(".agents", "skills", "fix-bug", "SKILL.md"),
	}))
	require.NoError(t, err)
	assert.Equal(t, "skill body", result.Text())
}
