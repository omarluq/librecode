package agent_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/agent"
	"github.com/omarluq/librecode/internal/core"
	"github.com/omarluq/librecode/internal/tool"
)

func TestLoadIncludesBuiltInExploreAgent(t *testing.T) {
	t.Parallel()

	catalog := agent.Load(t.TempDir())
	definition, found := catalog.Get(" EXPLORE ")

	require.True(t, found)
	assert.Equal(t, "explore", definition.Name)
	assert.Equal(
		t,
		[]tool.Name{tool.NameRead, tool.NameGrep, tool.NameFind, tool.NameLS, tool.NameAST},
		definition.Tools,
	)
	assert.Empty(t, catalog.Diagnostics())
}

func TestLoadProjectDefinitionOverridesBuiltIn(t *testing.T) {
	cwd := t.TempDir()
	t.Setenv("LIBRECODE_HOME", t.TempDir())
	writeDefinition(t, filepath.Join(cwd, core.ConfigDirName, "agents", "explore.md"), `---
name: explore
description: Project explorer
tools: [read]
---
Project-specific instructions.
`)

	catalog := agent.Load(cwd)
	definition, found := catalog.Get("explore")

	require.True(t, found)
	assert.Equal(t, "Project explorer", definition.Description)
	assert.Equal(t, []tool.Name{tool.NameRead}, definition.Tools)
	require.Len(t, catalog.Diagnostics(), 1)
	assert.Contains(t, catalog.Diagnostics()[0].Message, "shadowed")
}

func TestLoadRejectsUnknownAndMutatingTools(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tool string
		want string
	}{
		{name: "unknown", tool: "missing", want: "unknown tool"},
		{name: "mutating", tool: "write", want: "not read-only"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cwd := t.TempDir()
			writeDefinition(t, filepath.Join(cwd, core.ConfigDirName, "agents", "invalid.md"), `---
name: invalid
description: Invalid agent
tools: [`+testCase.tool+`]
---
Instructions.
`)

			catalog := agent.Load(cwd)
			_, found := catalog.Get("invalid")

			assert.False(t, found)
			require.Len(t, catalog.Diagnostics(), 1)
			assert.Contains(t, catalog.Diagnostics()[0].Message, testCase.want)
		})
	}
}

func writeDefinition(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}
